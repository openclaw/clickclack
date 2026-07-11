import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const directory = path.dirname(new URL(import.meta.url).pathname);
const repositoryRoot = path.resolve(directory, "../../..");
const ownerPath = path.join(directory, "owner.mjs");
const profilePath = path.join(directory, "profile.json");
const templatePath = path.join(directory, "template.json");
const bootstrapPath = path.join(directory, "bootstrap.sh");
const runbookPath = path.join(directory, "README.md");
const workflowPath = path.join(repositoryRoot, ".github/workflows/fakeco-aws.yml");
const ownerTemplate = JSON.parse(await readFile(templatePath, "utf8"));
const sourceCommit = "1ef89aafc874f267e2a432c633148b1c1b200d2a";
const ownerCommit = "a".repeat(40);
const kmsKeyArn = "arn:aws:kms:us-west-2:123456789012:key/12345678-1234-1234-1234-123456789abc";

test("profile and template lock the private ARM64 single-VM contract", async () => {
  const profile = JSON.parse(await readFile(profilePath, "utf8"));
  const template = JSON.parse(await readFile(templatePath, "utf8"));
  const result = runOwner(["validate-profile"]);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), {
    ok: true,
    stackName: "clickclack-fakeco",
    region: "us-west-2",
  });
  assert.equal(profile.defaultCommit, sourceCommit);
  assert.equal(profile.instance.type, "t4g.small");
  assert.equal(profile.instance.architecture, "arm64");
  assert.equal(profile.instance.rootVolumeGiB, 16);
  assert.equal(profile.network.preferredStackName, "crabhelm-fakeco");

  const resources = template.Resources;
  assert.equal(
    Object.values(resources).filter((resource) => resource.Type === "AWS::EC2::Instance").length,
    1,
  );
  for (const forbidden of [
    "AWS::EC2::VPC",
    "AWS::EC2::Subnet",
    "AWS::EC2::NatGateway",
    "AWS::EC2::EIP",
    "AWS::EC2::InternetGateway",
    "AWS::EC2::Route",
    "AWS::EC2::RouteTable",
  ]) {
    assert.equal(
      Object.values(resources).some((resource) => resource.Type === forbidden),
      false,
    );
  }
  const instance = resources.Instance.Properties;
  assert.equal(instance.InstanceType, "t4g.small");
  assert.deepEqual(instance.CreditSpecification, { CPUCredits: "standard" });
  assert.equal(instance.NetworkInterfaces[0].AssociatePublicIpAddress, false);
  assert.equal(instance.KeyName, undefined);
  assert.equal(instance.MetadataOptions.HttpTokens, "required");
  assert.equal(instance.MetadataOptions.HttpPutResponseHopLimit, 1);
  assert.equal(instance.PropagateTagsToVolumeOnCreation, true);
  assert.deepEqual(instance.BlockDeviceMappings, [
    {
      DeviceName: "/dev/sda1",
      Ebs: {
        DeleteOnTermination: false,
        Encrypted: true,
        KmsKeyId: { Ref: "DataKmsKeyArn" },
        VolumeSize: 16,
        VolumeType: "gp3",
      },
    },
  ]);
  assert.deepEqual(template.Outputs.InstanceProfileArn.Value, {
    "Fn::GetAtt": ["InstanceProfile", "Arn"],
  });
  assert.deepEqual(template.Outputs.InstanceProfileName.Value, { Ref: "InstanceProfile" });
  assert.deepEqual(template.Outputs.InstanceRoleName.Value, { Ref: "InstanceRole" });
  const ingresses = [resources.GatewayIngress, resources.MetricsIngress];
  for (const ingress of ingresses) {
    assert.equal(ingress.Properties.IpProtocol, "tcp");
    assert.equal(ingress.Properties.FromPort, 8080);
    assert.equal(ingress.Properties.ToPort, 8080);
    assert.equal(ingress.Properties.CidrIp, undefined);
    assert.ok(ingress.Properties.SourceSecurityGroupId.Ref);
  }
});

test("instance role contains standard SSM core plus exact object destinations only", async () => {
  const template = JSON.parse(await readFile(templatePath, "utf8"));
  const role = template.Resources.InstanceRole.Properties;
  assert.deepEqual(role.PermissionsBoundary, { Ref: "WorkloadPermissionsBoundaryArn" });
  assert.equal(role.ManagedPolicyArns, undefined);
  assert.deepEqual(
    role.Policies.map((policy) => policy.PolicyName),
    ["SsmManagedInstanceCore", "ExactFakeCoObjects"],
  );
  const objectStatements = role.Policies[1].PolicyDocument.Statement;
  assert.deepEqual(objectStatements[0], {
    Sid: "ListRemoteScriptPrefix",
    Effect: "Allow",
    Action: ["s3:ListBucket"],
    Resource: { Ref: "ArtifactBucketArn" },
    Condition: {
      StringLike: {
        "s3:prefix": { "Fn::Sub": "${ArtifactPrefix}/owner/*" },
      },
    },
  });
  assert.deepEqual(objectStatements[2].Action, ["s3:AbortMultipartUpload", "s3:PutObject"]);
  assert.deepEqual(objectStatements[3].Action, [
    "s3:AbortMultipartUpload",
    "s3:GetObject",
    "s3:PutObject",
  ]);
  assert.deepEqual(
    objectStatements.map((statement) => statement.Resource),
    [
      { Ref: "ArtifactBucketArn" },
      { "Fn::Sub": "${ArtifactBucketArn}/${ArtifactPrefix}/*" },
      { "Fn::Sub": "${LogBucketArn}/${LogPrefix}/*" },
      { "Fn::Sub": "${BackupBucketArn}/${BackupPrefix}/*" },
      { Ref: "DataKmsKeyArn" },
    ],
  );
  const serialized = JSON.stringify(role);
  assert.doesNotMatch(serialized, /secretsmanager|dynamodb|cloudwatch|logs:/iu);
  assert.doesNotMatch(serialized, /ssm:GetParameters?/u);
  assert.doesNotMatch(serialized, /s3:\*/u);
  assert.doesNotMatch(serialized, /s3:ListAllMyBuckets/u);
});

test("KMS key policy verification requires target-account IAM delegation", async (t) => {
  const temporary = await temporaryDirectory(t);
  const policyPath = path.join(temporary, "kms-policy.json");
  const policy = {
    Version: "2012-10-17",
    Statement: [
      {
        Effect: "Allow",
        Principal: { AWS: "arn:aws:iam::123456789012:root" },
        Action: [
          "kms:CreateGrant",
          "kms:Decrypt",
          "kms:DescribeKey",
          "kms:Encrypt",
          "kms:GenerateDataKey*",
          "kms:GetKeyPolicy",
          "kms:ReEncrypt*",
        ],
        Resource: "*",
      },
    ],
  };
  await writeFile(policyPath, JSON.stringify(policy));
  const valid = runOwner([
    "verify-kms-policy",
    "--policy",
    policyPath,
    "--account-id",
    "123456789012",
  ]);
  assert.equal(valid.status, 0, valid.stderr);
  assert.deepEqual(JSON.parse(valid.stdout), { ok: true, accountId: "123456789012" });

  policy.Statement[0].Condition = { Bool: { "kms:GrantIsForAWSResource": "true" } };
  await writeFile(policyPath, JSON.stringify(policy));
  const conditioned = runOwner([
    "verify-kms-policy",
    "--policy",
    policyPath,
    "--account-id",
    "123456789012",
  ]);
  assert.notEqual(conditioned.status, 0);
  assert.match(conditioned.stderr, /must delegate required actions/u);
});

test("render emits a secret-free exact deployment and parameter file", async (t) => {
  const temporary = await temporaryDirectory(t);
  const renderedPath = path.join(temporary, "rendered.json");
  const parametersPath = path.join(temporary, "parameters.json");
  const result = runOwner(
    [
      "render",
      "--phase",
      "apply",
      "--commit",
      sourceCommit,
      "--owner-commit",
      ownerCommit,
      "--output",
      renderedPath,
    ],
    fakecoEnvironment(),
  );
  assert.equal(result.status, 0, result.stderr);
  const rendered = JSON.parse(await readFile(renderedPath, "utf8"));
  assert.equal(rendered.target.accountId, "123456789012");
  assert.equal(rendered.target.region, "us-west-2");
  assert.equal(rendered.target.vpcId, "vpc-1234abcd");
  assert.equal(rendered.target.subnetId, "subnet-1234abcd");
  assert.equal(rendered.target.egressResourceId, "nat-1234abcd");
  assert.equal(rendered.source.commit, sourceCommit);
  assert.equal(
    rendered.source.artifactKey,
    `clickclack/fakeco/artifacts/${sourceCommit}/source.tar.gz`,
  );
  assert.equal(
    rendered.source.bootstrapKey,
    `clickclack/fakeco/artifacts/owner/${ownerCommit}/bootstrap.sh`,
  );
  assert.deepEqual(rendered.tags, [
    { Key: "Environment", Value: "fakeco" },
    { Key: "ManagedBy", Value: "github-actions" },
    { Key: "Project", Value: "clickclack" },
  ]);
  const serialized = JSON.stringify(rendered);
  for (const forbidden of [
    "CLICKCLACK_TOKEN",
    "CLAWROUTER_API_KEY",
    "OPENCLAW_GATEWAY_TOKEN",
    "SecretString",
  ]) {
    assert.doesNotMatch(serialized, new RegExp(forbidden, "u"));
  }

  const parameters = runOwner([
    "parameters",
    "--rendered",
    renderedPath,
    "--output",
    parametersPath,
  ]);
  assert.equal(parameters.status, 0, parameters.stderr);
  const parameterList = JSON.parse(await readFile(parametersPath, "utf8"));
  assert.equal(parameterList.length, 14);
  assert.deepEqual(parameterList, rendered.parameters);
});

test("render fails closed on target, trust-boundary, and prefix drift", async (t) => {
  const temporary = await temporaryDirectory(t);
  const sharedBucketEnvironment = fakecoEnvironment();
  sharedBucketEnvironment.FAKECO_LOG_BUCKET = sharedBucketEnvironment.FAKECO_ARTIFACT_BUCKET;
  sharedBucketEnvironment.FAKECO_BACKUP_BUCKET = sharedBucketEnvironment.FAKECO_ARTIFACT_BUCKET;
  const sharedBucket = runOwner(
    [
      "render",
      "--phase",
      "apply",
      "--commit",
      sourceCommit,
      "--owner-commit",
      ownerCommit,
      "--output",
      path.join(temporary, "shared-bucket.json"),
    ],
    sharedBucketEnvironment,
  );
  assert.equal(sharedBucket.status, 0, sharedBucket.stderr);

  const cases = [
    ["region", { FAKECO_AWS_REGION: "us-east-1" }, /must equal us-west-2/u],
    [
      "role path",
      { FAKECO_GITHUB_ROLE_ARN: "arn:aws:iam::123456789012:role/Administrator" },
      /locked FakeCo path/u,
    ],
    [
      "public egress",
      { FAKECO_EGRESS_RESOURCE_ID: "igw-1234abcd" },
      /egress resource ID is invalid/u,
    ],
    [
      "CIDR source",
      { FAKECO_OPENCLAW_GATEWAY_SECURITY_GROUP_ID: "10.0.0.0/8" },
      /security group ID is invalid/u,
    ],
    [
      "duplicate ingress sources",
      { FAKECO_METRICS_SECURITY_GROUP_ID: "sg-1234abcd" },
      /metrics security group must differ/u,
    ],
    [
      "broad prefix",
      { FAKECO_BACKUP_PREFIX: "clickclack" },
      /normalized clickclack\/fakeco prefix/u,
    ],
    [
      "unsafe prefix",
      { FAKECO_LOG_PREFIX: "clickclack/fakeco/logs/*" },
      /normalized clickclack\/fakeco prefix/u,
    ],
    [
      "equal shared-bucket prefixes",
      {
        FAKECO_LOG_BUCKET: "openclaw-fakeco-artifact-123456789012",
        FAKECO_LOG_PREFIX: "clickclack/fakeco/artifacts",
      },
      /prefixes must not overlap when buckets are shared/u,
    ],
    [
      "nested shared-bucket prefixes",
      {
        FAKECO_BACKUP_BUCKET: "openclaw-fakeco-logs-123456789012",
        FAKECO_BACKUP_PREFIX: "clickclack/fakeco/logs/backups",
      },
      /prefixes must not overlap when buckets are shared/u,
    ],
    [
      "cross-account key",
      {
        FAKECO_DATA_KMS_KEY_ARN:
          "arn:aws:kms:us-west-2:999999999999:key/12345678-1234-1234-1234-123456789abc",
      },
      /target account and region/u,
    ],
  ];
  for (const [label, overrides, expected] of cases) {
    const result = runOwner(
      [
        "render",
        "--phase",
        "apply",
        "--commit",
        sourceCommit,
        "--owner-commit",
        ownerCommit,
        "--output",
        path.join(temporary, `${label}.json`),
      ],
      { ...fakecoEnvironment(), ...overrides },
    );
    assert.notEqual(result.status, 0, label);
    assert.match(result.stderr, expected, label);
  }
});

test("stack, instance, backup, and retention replay verify the observed resources", async (t) => {
  const temporary = await temporaryDirectory(t);
  const renderedPath = path.join(temporary, "rendered.json");
  assert.equal(
    runOwner(
      [
        "render",
        "--phase",
        "teardown",
        "--commit",
        sourceCommit,
        "--owner-commit",
        ownerCommit,
        "--output",
        renderedPath,
      ],
      fakecoEnvironment(),
    ).status,
    0,
  );
  const rendered = JSON.parse(await readFile(renderedPath, "utf8"));
  const stackPath = path.join(temporary, "stack.json");
  const instancePath = path.join(temporary, "instance.json");
  const volumePath = path.join(temporary, "volume.json");
  const securityGroupPath = path.join(temporary, "security-group.json");
  const workloadIAMPath = path.join(temporary, "workload-iam.json");
  const evidencePath = path.join(temporary, "evidence.json");
  const snapshotPath = path.join(temporary, "snapshot.json");
  const retentionPath = path.join(temporary, "retention.json");
  const inventoryArgs = await stackInventoryArgs(temporary, rendered);
  await writeFile(stackPath, JSON.stringify(stackResponse(rendered)));
  await writeFile(instancePath, JSON.stringify(instanceResponse()));
  await writeFile(volumePath, JSON.stringify(volumeResponse()));
  await writeFile(securityGroupPath, JSON.stringify(securityGroupResponse(rendered)));
  await writeFile(workloadIAMPath, JSON.stringify(workloadIAMResponse(rendered)));
  await writeFile(evidencePath, JSON.stringify(backupEvidence(rendered)));
  await writeFile(snapshotPath, JSON.stringify(snapshotResponse()));

  assert.equal(
    runOwner(["verify-stack", "--rendered", renderedPath, "--stack", stackPath, ...inventoryArgs])
      .status,
    0,
  );
  assert.equal(
    runOwner([
      "verify-instance",
      "--rendered",
      renderedPath,
      "--stack",
      stackPath,
      ...inventoryArgs,
      "--instance",
      instancePath,
      "--volume",
      volumePath,
      "--security-group",
      securityGroupPath,
      "--workload-iam",
      workloadIAMPath,
    ]).status,
    0,
  );
  assert.equal(
    runOwner(["verify-backup", "--rendered", renderedPath, "--evidence", evidencePath]).status,
    0,
  );
  const staleEvidence = backupEvidence(rendered);
  staleEvidence.runtime_commit_verified = false;
  await writeFile(evidencePath, JSON.stringify(staleEvidence));
  const stale = runOwner(["verify-backup", "--rendered", renderedPath, "--evidence", evidencePath]);
  assert.notEqual(stale.status, 0);
  assert.match(stale.stderr, /runtime commit proof/u);
  const incompleteSeedEvidence = backupEvidence(rendered);
  delete incompleteSeedEvidence.seed_manifest_sha256;
  await writeFile(evidencePath, JSON.stringify(incompleteSeedEvidence));
  const incompleteSeed = runOwner([
    "verify-backup",
    "--rendered",
    renderedPath,
    "--evidence",
    evidencePath,
  ]);
  assert.notEqual(incompleteSeed.status, 0);
  assert.match(incompleteSeed.stderr, /seed manifest SHA-256/u);
  const runningCommit = "e".repeat(40);
  const rollbackEvidence = backupEvidence(rendered, runningCommit);
  await writeFile(evidencePath, JSON.stringify(rollbackEvidence));
  assert.equal(
    runOwner(["verify-backup", "--rendered", renderedPath, "--evidence", evidencePath]).status,
    0,
    "teardown must accept a separately verified running commit after a failed update",
  );
  const verifyRenderedPath = path.join(temporary, "verify-rendered.json");
  await writeFile(verifyRenderedPath, JSON.stringify({ ...rendered, phase: "verify" }));
  const verifyMismatch = runOwner([
    "verify-backup",
    "--rendered",
    verifyRenderedPath,
    "--evidence",
    evidencePath,
  ]);
  assert.notEqual(verifyMismatch.status, 0);
  assert.match(verifyMismatch.stderr, /backup action drifted/u);
  const verifyRuntimeMismatchEvidence = { ...rollbackEvidence, action: "verify" };
  await writeFile(evidencePath, JSON.stringify(verifyRuntimeMismatchEvidence));
  const verifyRuntimeMismatch = runOwner([
    "verify-backup",
    "--rendered",
    verifyRenderedPath,
    "--evidence",
    evidencePath,
  ]);
  assert.notEqual(verifyRuntimeMismatch.status, 0);
  assert.match(verifyRuntimeMismatch.stderr, /backup source commit drifted/u);
  const stackDriftEvidence = backupEvidence(rendered, runningCommit);
  stackDriftEvidence.stack_source_commit = "f".repeat(40);
  await writeFile(evidencePath, JSON.stringify(stackDriftEvidence));
  const stackDrift = runOwner([
    "verify-backup",
    "--rendered",
    renderedPath,
    "--evidence",
    evidencePath,
  ]);
  assert.notEqual(stackDrift.status, 0);
  assert.match(stackDrift.stderr, /stack source commit drifted/u);
  await writeFile(evidencePath, JSON.stringify(rollbackEvidence));
  const retention = runOwner([
    "retention-manifest",
    "--rendered",
    renderedPath,
    "--stack",
    stackPath,
    ...inventoryArgs,
    "--instance",
    instancePath,
    "--volume",
    volumePath,
    "--security-group",
    securityGroupPath,
    "--workload-iam",
    workloadIAMPath,
    "--snapshot",
    snapshotPath,
    "--backup-evidence",
    evidencePath,
    "--output",
    retentionPath,
  ]);
  assert.equal(retention.status, 0, retention.stderr);
  const manifest = JSON.parse(await readFile(retentionPath, "utf8"));
  assert.equal(manifest.retained.root_volume.delete_on_termination, false);
  assert.equal(manifest.retained.root_volume.id, "vol-1234abcd");
  assert.equal(manifest.retained.snapshot.id, "snap-1234abcd");
  assert.equal(manifest.retained.sqlite_backup.sha256, "b".repeat(64));
  assert.equal(manifest.source_commit, sourceCommit);
  assert.equal(manifest.runtime_source_commit, runningCommit);
  assert.deepEqual(manifest.deletion_contract, {
    cloudformation_mode: "STANDARD",
    snapshots_deleted: false,
    s3_objects_deleted: false,
    root_volume_delete_on_termination: false,
  });
});

test("observed drift is rejected without echoing private values", async (t) => {
  const temporary = await temporaryDirectory(t);
  const renderedPath = path.join(temporary, "rendered.json");
  runOwner(
    [
      "render",
      "--phase",
      "verify",
      "--commit",
      sourceCommit,
      "--owner-commit",
      ownerCommit,
      "--output",
      renderedPath,
    ],
    fakecoEnvironment(),
  );
  const rendered = JSON.parse(await readFile(renderedPath, "utf8"));
  const stack = stackResponse(rendered);
  stack.Stacks[0].Parameters.find((entry) => entry.ParameterKey === "VpcId").ParameterValue =
    "vpc-private-drift";
  const stackPath = path.join(temporary, "stack.json");
  const inventoryArgs = await stackInventoryArgs(temporary, rendered);
  await writeFile(stackPath, JSON.stringify(stack));
  const result = runOwner([
    "verify-stack",
    "--rendered",
    renderedPath,
    "--stack",
    stackPath,
    ...inventoryArgs,
  ]);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /parameter VpcId drifted/u);
  assert.doesNotMatch(result.stderr, /vpc-private-drift/u);
  assert.doesNotMatch(result.stderr, /vpc-1234abcd/u);
});

test("stack replay accepts stable rollback and rejects template or resource drift", async (t) => {
  const temporary = await temporaryDirectory(t);
  const renderedPath = path.join(temporary, "rendered.json");
  assert.equal(
    runOwner(
      [
        "render",
        "--phase",
        "teardown",
        "--commit",
        sourceCommit,
        "--owner-commit",
        ownerCommit,
        "--output",
        renderedPath,
      ],
      fakecoEnvironment(),
    ).status,
    0,
  );
  const rendered = JSON.parse(await readFile(renderedPath, "utf8"));
  const stackPath = path.join(temporary, "stack.json");
  const templatePath = path.join(temporary, "observed-template.json");
  const resourcesPath = path.join(temporary, "observed-resources.json");
  const stack = stackResponse(rendered);
  stack.Stacks[0].StackStatus = "UPDATE_ROLLBACK_COMPLETE";
  await writeFile(stackPath, JSON.stringify(stack));
  await writeFile(templatePath, JSON.stringify(ownerTemplate));
  await writeFile(resourcesPath, JSON.stringify(stackResourcesResponse(rendered)));
  const args = [
    "verify-stack",
    "--rendered",
    renderedPath,
    "--stack",
    stackPath,
    "--template",
    templatePath,
    "--resources",
    resourcesPath,
  ];
  assert.equal(runOwner(args).status, 0, "stable rollback-complete stack must be recoverable");

  await writeFile(
    templatePath,
    JSON.stringify({ ...ownerTemplate, Description: "out-of-band template" }),
  );
  const templateDrift = runOwner(args);
  assert.notEqual(templateDrift.status, 0);
  assert.match(templateDrift.stderr, /stack template drifted/u);

  await writeFile(templatePath, JSON.stringify(ownerTemplate));
  const resources = stackResourcesResponse(rendered);
  resources.StackResources.push({
    StackName: "clickclack-fakeco",
    LogicalResourceId: "UnexpectedVolume",
    PhysicalResourceId: "vol-private-extra",
    ResourceType: "AWS::EC2::Volume",
    ResourceStatus: "CREATE_COMPLETE",
  });
  await writeFile(resourcesPath, JSON.stringify(resources));
  const resourceDrift = runOwner(args);
  assert.notEqual(resourceDrift.status, 0);
  assert.match(resourceDrift.stderr, /stack resource set drifted/u);
  assert.doesNotMatch(resourceDrift.stderr, /vol-private-extra/u);
});

test("live security group drift is rejected", async (t) => {
  const temporary = await temporaryDirectory(t);
  const renderedPath = path.join(temporary, "rendered.json");
  assert.equal(
    runOwner(
      [
        "render",
        "--phase",
        "verify",
        "--commit",
        sourceCommit,
        "--owner-commit",
        ownerCommit,
        "--output",
        renderedPath,
      ],
      fakecoEnvironment(),
    ).status,
    0,
  );
  const rendered = JSON.parse(await readFile(renderedPath, "utf8"));
  const stackPath = path.join(temporary, "stack.json");
  const instancePath = path.join(temporary, "instance.json");
  const volumePath = path.join(temporary, "volume.json");
  const securityGroupPath = path.join(temporary, "security-group.json");
  const workloadIAMPath = path.join(temporary, "workload-iam.json");
  const inventoryArgs = await stackInventoryArgs(temporary, rendered);
  await writeFile(stackPath, JSON.stringify(stackResponse(rendered)));
  await writeFile(instancePath, JSON.stringify(instanceResponse()));
  await writeFile(volumePath, JSON.stringify(volumeResponse()));
  await writeFile(workloadIAMPath, JSON.stringify(workloadIAMResponse(rendered)));

  const cases = [
    [
      "IPv4 CIDR",
      (response) => {
        response.SecurityGroups[0].IpPermissions[0].IpRanges = [{ CidrIp: "0.0.0.0/0" }];
      },
      /IPv4 CIDR ingress is forbidden/u,
    ],
    [
      "IPv6 CIDR",
      (response) => {
        response.SecurityGroups[0].IpPermissions[0].Ipv6Ranges = [{ CidrIpv6: "::/0" }];
      },
      /IPv6 CIDR ingress is forbidden/u,
    ],
    [
      "port",
      (response) => {
        response.SecurityGroups[0].IpPermissions[0].FromPort = 22;
      },
      /ingress port drifted/u,
    ],
    [
      "protocol",
      (response) => {
        response.SecurityGroups[0].IpPermissions[0].IpProtocol = "-1";
      },
      /ingress protocol drifted/u,
    ],
    [
      "source",
      (response) => {
        response.SecurityGroups[0].IpPermissions[0].UserIdGroupPairs[0].GroupId = "sg-deadbeef";
      },
      /source security group drifted/u,
    ],
    [
      "source account",
      (response) => {
        delete response.SecurityGroups[0].IpPermissions[0].UserIdGroupPairs[0].UserId;
      },
      /source account drifted/u,
    ],
  ];
  for (const [label, mutate, expected] of cases) {
    const response = securityGroupResponse(rendered);
    mutate(response);
    await writeFile(securityGroupPath, JSON.stringify(response));
    const result = runOwner([
      "verify-instance",
      "--rendered",
      renderedPath,
      "--stack",
      stackPath,
      ...inventoryArgs,
      "--instance",
      instancePath,
      "--volume",
      volumePath,
      "--security-group",
      securityGroupPath,
      "--workload-iam",
      workloadIAMPath,
    ]);
    assert.notEqual(result.status, 0, label);
    assert.match(result.stderr, expected, label);
  }
});

test("live workload IAM drift is rejected", async (t) => {
  const temporary = await temporaryDirectory(t);
  const renderedPath = path.join(temporary, "rendered.json");
  assert.equal(
    runOwner(
      [
        "render",
        "--phase",
        "verify",
        "--commit",
        sourceCommit,
        "--owner-commit",
        ownerCommit,
        "--output",
        renderedPath,
      ],
      fakecoEnvironment(),
    ).status,
    0,
  );
  const rendered = JSON.parse(await readFile(renderedPath, "utf8"));
  const stackPath = path.join(temporary, "stack.json");
  const instancePath = path.join(temporary, "instance.json");
  const volumePath = path.join(temporary, "volume.json");
  const securityGroupPath = path.join(temporary, "security-group.json");
  const workloadIAMPath = path.join(temporary, "workload-iam.json");
  const inventoryArgs = await stackInventoryArgs(temporary, rendered);
  await writeFile(stackPath, JSON.stringify(stackResponse(rendered)));
  await writeFile(volumePath, JSON.stringify(volumeResponse()));
  await writeFile(securityGroupPath, JSON.stringify(securityGroupResponse(rendered)));

  const cases = [
    [
      "additional block device",
      (instance) => {
        instance.Reservations[0].Instances[0].BlockDeviceMappings.push({
          DeviceName: "/dev/sdf",
          Ebs: { VolumeId: "vol-private-extra", DeleteOnTermination: true },
        });
      },
      () => {},
      /exactly one block device mapping/u,
    ],
    [
      "instance association",
      (instance) => {
        instance.Reservations[0].Instances[0].IamInstanceProfile.Arn =
          "arn:aws:iam::123456789012:instance-profile/Administrator";
      },
      () => {},
      /instance profile drifted/u,
    ],
    [
      "boundary",
      () => {},
      (iam) => {
        iam.Role.PermissionsBoundary.PermissionsBoundaryArn =
          "arn:aws:iam::123456789012:policy/Administrator";
      },
      /permissions boundary drifted/u,
    ],
    [
      "managed policy",
      () => {},
      (iam) => {
        iam.AttachedPolicies = [
          {
            PolicyName: "AdministratorAccess",
            PolicyArn: "arn:aws:iam::aws:policy/AdministratorAccess",
          },
        ];
      },
      /managed policies are forbidden/u,
    ],
    [
      "inline policy",
      () => {},
      (iam) => {
        iam.RolePolicies.find(
          (policy) => policy.PolicyName === "ExactFakeCoObjects",
        ).PolicyDocument.Statement[0].Action = ["s3:*"];
      },
      /inline policy ExactFakeCoObjects drifted/u,
    ],
    [
      "unexpected customer tag",
      () => {},
      (iam) => {
        iam.Role.Tags.push({ Key: "Owner", Value: "unexpected" });
      },
      /workload role tag set drifted/u,
    ],
  ];
  for (const [label, mutateInstance, mutateIAM, expected] of cases) {
    const instance = instanceResponse();
    const workloadIAM = workloadIAMResponse(rendered);
    mutateInstance(instance);
    mutateIAM(workloadIAM);
    await writeFile(instancePath, JSON.stringify(instance));
    await writeFile(workloadIAMPath, JSON.stringify(workloadIAM));
    const result = runOwner([
      "verify-instance",
      "--rendered",
      renderedPath,
      "--stack",
      stackPath,
      ...inventoryArgs,
      "--instance",
      instancePath,
      "--volume",
      volumePath,
      "--security-group",
      securityGroupPath,
      "--workload-iam",
      workloadIAMPath,
    ]);
    assert.notEqual(result.status, 0, label);
    assert.match(result.stderr, expected, label);
  }
});

test("manual workflow is protected, change-set-first, bounded, and deletion-safe", async () => {
  const workflow = await readFile(workflowPath, "utf8");
  assert.match(workflow, /workflow_dispatch:/u);
  assert.doesNotMatch(workflow, /^\s{2}(push|pull_request|schedule):/mu);
  assert.match(workflow, /environment: fakeco/u);
  assert.match(workflow, /github\.ref_protected/u);
  assert.match(workflow, /allowed-account-ids:/u);
  assert.match(workflow, /aws-region: us-west-2/u);
  assert.match(workflow, /role-to-assume: \$\{\{ vars\.FAKECO_GITHUB_ROLE_ARN \}\}/u);
  assert.doesNotMatch(workflow, /secrets\./u);
  assert.doesNotMatch(workflow, /CLICKCLACK_TOKEN|CLAWROUTER_API_KEY|OPENCLAW_GATEWAY_TOKEN/u);
  assert.match(workflow, /timeout-minutes: 90/u);
  assert.ok(
    workflow.indexOf("create-change-set") < workflow.indexOf("execute-change-set"),
    "change set must be created before it can execute",
  );
  assert.match(workflow, /seq 1 60/u);
  assert.match(workflow, /seq 1 240/u);
  assert.match(workflow, /executionTimeout: \["2400"\]/u);
  assert.match(workflow, /verify-kms-policy/u);
  assert.match(workflow, /simulate-principal-policy/u);
  assert.match(workflow, /kms:GrantIsForAWSResource/u);
  assert.match(workflow, /kms:GenerateDataKeyWithoutPlaintext/u);
  assert.match(workflow, /unscoped-create-grant/u);
  assert.match(workflow, /AWS-RunRemoteScript/u);
  assert.match(workflow, /::add-mask::%s/u);
  assert.match(workflow, /\.Action == "Remove"/u);
  assert.match(workflow, /\.Replacement == "True"/u);
  assert.match(workflow, /\.Replacement == "Conditional"/u);
  assert.ok(
    workflow.indexOf("Apply refuses removals") < workflow.indexOf("execute-change-set"),
    "destructive change guard must run before execution",
  );
  assert.match(workflow, /--checksum-algorithm SHA256/u);
  assert.match(workflow, /verified_object/u);
  assert.match(workflow, /sha256sum "\$verified_object"/u);
  assert.match(workflow, /sha256sum --check --status && env/u);
  assert.match(workflow, /describe-security-groups/u);
  assert.match(workflow, /\.Vpcs\[0\]\.OwnerId == \$owner/u);
  assert.match(workflow, /\.Subnets\[0\]\.OwnerId/u);
  assert.match(workflow, /\.SecurityGroups\[0\]\.OwnerId == \$owner/u);
  assert.match(workflow, /\.NatGatewayAddresses\[\]\.NetworkInterfaceId/u);
  assert.match(workflow, /LC_ALL=C sort -u/u);
  assert.match(workflow, /describe-network-interfaces/u);
  assert.match(workflow, /all\(\.NetworkInterfaces\[\]; \.OwnerId == \$owner/u);
  assert.match(workflow, /\.TransitGateways\[0\]\.OwnerId/u);
  assert.match(workflow, /cloudformation get-template/u);
  assert.match(workflow, /cloudformation describe-stack-resources/u);
  assert.match(workflow, /--template "\$template_file"/u);
  assert.match(workflow, /--resources "\$resources_file"/u);
  assert.match(workflow, /--security-group "\$security_group_file"/u);
  assert.match(workflow, /get-instance-profile/u);
  assert.match(workflow, /list-attached-role-policies/u);
  assert.match(workflow, /get-role-policy/u);
  assert.match(workflow, /--workload-iam "\$workload_iam_file"/u);
  assert.match(workflow, /\.State == "active"/u);
  assert.ok(
    workflow.indexOf("sha256sum --check --status && env") <
      workflow.indexOf("bash bootstrap.sh %s"),
    "the instance must verify bootstrap bytes before root execution",
  );
  assert.ok(
    workflow.indexOf("create-snapshot") < workflow.lastIndexOf("delete-stack"),
    "snapshot must complete before stack deletion",
  );
  assert.ok(
    workflow.lastIndexOf("cloudformation describe-stack-resources") <
      workflow.indexOf("create-snapshot"),
    "the complete stack inventory must be refreshed before retention begins",
  );
  assert.match(workflow, /seq 1 180/u);
  assert.match(workflow, /snapshot_state/u);
  assert.doesNotMatch(workflow, /ec2 wait snapshot-completed/u);
  assert.match(workflow, /destroy-clickclack-fakeco-retain-data/u);
  assert.match(workflow, /deletion-mode STANDARD/u);
  assert.doesNotMatch(
    workflow,
    /delete-snapshot|delete-volume|delete-object|s3 rm|down --volumes/u,
  );
});

test("teardown backup resolves and verifies the actually running release", async (t) => {
  const temporary = await temporaryDirectory(t);
  const bootstrap = await readFile(bootstrapPath, "utf8");
  const functionsStart = bootstrap.indexOf("set_runtime_paths() {");
  const functionsEnd = bootstrap.indexOf("\ninstall_aws_cli() {", functionsStart);
  assert.notEqual(functionsStart, -1);
  assert.notEqual(functionsEnd, -1);
  const runtimeFunctions = bootstrap.slice(functionsStart, functionsEnd).trim();
  const requested = "a".repeat(40);
  const running = "b".repeat(40);
  const sourceDigest = "c".repeat(64);
  const imageID = `sha256:${"d".repeat(64)}`;
  const releaseRoot = path.join(temporary, "releases");
  const stateRoot = path.join(temporary, "state");
  const bin = path.join(temporary, "bin");
  await mkdir(path.join(releaseRoot, running), { recursive: true });
  await mkdir(stateRoot, { recursive: true });
  await mkdir(bin, { recursive: true });
  await writeFile(path.join(releaseRoot, running, ".source.sha256"), `${sourceDigest}\n`);
  await writeFile(path.join(stateRoot, `image-${running}.id`), `${imageID}\n`);
  await writeFile(
    path.join(temporary, "compose.owner.yaml"),
    `services:\n  app:\n    image: "clickclack:fakeco-${requested}"\n  seed:\n    image: "clickclack:fakeco-${requested}"\n`,
  );
  await writeFile(path.join(temporary, "runtime.env"), `CLICKCLACK_WEB_VERSION=${requested}\n`);
  await writeFile(
    path.join(bin, "docker"),
    `#!/usr/bin/env bash
set -euo pipefail
case "\${1:-}" in
  ps)
    if [[ "\${NO_CONTAINERS:-false}" == true ]]; then
      exit 0
    fi
    printf '%s\n' running-container
    if [[ "\${MULTIPLE_CONTAINERS:-false}" == true ]]; then
      printf '%s\n' second-container
    fi
    ;;
  inspect)
    case "\${3:-}" in
      '{{.Config.Image}}')
        printf '%s\n' "\${CONFIGURED_IMAGE:-clickclack:fakeco-$RUNNING_COMMIT}"
        ;;
      '{{.Image}}') printf '%s\n' "\${RUNNING_IMAGE_ID:-$IMAGE_ID}" ;;
      *) exit 64 ;;
    esac
    ;;
  image)
    [[ "\${2:-}" == inspect && "\${3:-}" == --format ]]
    case "\${4:-}" in
      '{{.Id}}') printf '%s\n' "\${LOCAL_IMAGE_ID:-$IMAGE_ID}" ;;
      '{{index .Config.Labels "org.opencontainers.image.revision"}}')
        printf '%s\n' "\${OCI_REVISION:-$RUNNING_COMMIT}"
        ;;
      *) exit 64 ;;
    esac
    ;;
  *) exit 64 ;;
esac
`,
    { mode: 0o755 },
  );
  const scriptPath = path.join(temporary, "resolve-runtime.sh");
  await writeFile(
    scriptPath,
    `#!/usr/bin/env bash
set -euo pipefail
${runtimeFunctions}
stage=initialize
release_root="$TEST_ROOT/releases"
state_root="$TEST_ROOT/state"
runtime_override="$TEST_ROOT/compose.owner.yaml"
runtime_env="$TEST_ROOT/runtime.env"
backup_runtime_override="$state_root/compose.backup.yaml"
compose_override="$runtime_override"
action=backup
requested_source_commit="$REQUESTED_COMMIT"
runtime_source_commit="$requested_source_commit"
runtime_source_sha256="$REQUESTED_SHA256"
set_runtime_paths
resolve_backup_runtime
[[ "$runtime_source_commit" == "$RUNNING_COMMIT" ]]
[[ "$runtime_source_sha256" == "$RUNNING_SHA256" ]]
[[ "$release" == "$release_root/$RUNNING_COMMIT" ]]
[[ "$image_state" == "$state_root/image-$RUNNING_COMMIT.id" ]]
[[ "$compose_override" == "$backup_runtime_override" ]]
grep -Fx "    image: \\"clickclack:fakeco-$RUNNING_COMMIT\\"" "$backup_runtime_override"
verify_persistent_runtime_config
`,
    { mode: 0o755 },
  );
  const environment = {
    ...process.env,
    PATH: `${bin}:${process.env.PATH}`,
    TEST_ROOT: temporary,
    REQUESTED_COMMIT: requested,
    REQUESTED_SHA256: "e".repeat(64),
    RUNNING_COMMIT: running,
    RUNNING_SHA256: sourceDigest,
    IMAGE_ID: imageID,
  };
  const runResolver = (overrides = {}) =>
    spawnSync("bash", [scriptPath], {
      encoding: "utf8",
      env: { ...environment, ...overrides },
    });
  const requestedConfig = `services:\n  app:\n    image: "clickclack:fakeco-${requested}"\n  seed:\n    image: "clickclack:fakeco-${requested}"\n`;

  await t.test("accepts a verified runtime with requested persistent pins", () => {
    const resolved = runResolver();
    assert.equal(resolved.status, 0, `${resolved.stdout}\n${resolved.stderr}`);
  });

  await t.test("accepts a verified runtime with observed persistent pins", async () => {
    await writeFile(
      path.join(temporary, "compose.owner.yaml"),
      `services:\n  app:\n    image: "clickclack:fakeco-${running}"\n  seed:\n    image: "clickclack:fakeco-${running}"\n`,
    );
    await writeFile(path.join(temporary, "runtime.env"), `CLICKCLACK_WEB_VERSION=${running}\n`);
    const resolved = runResolver();
    assert.equal(resolved.status, 0, `${resolved.stdout}\n${resolved.stderr}`);
    await writeFile(path.join(temporary, "compose.owner.yaml"), requestedConfig);
    await writeFile(path.join(temporary, "runtime.env"), `CLICKCLACK_WEB_VERSION=${requested}\n`);
  });

  await t.test("rejects mixed requested and observed persistent image pins", async () => {
    await writeFile(
      path.join(temporary, "compose.owner.yaml"),
      `services:\n  app:\n    image: "clickclack:fakeco-${requested}"\n  seed:\n    image: "clickclack:fakeco-${running}"\n`,
    );
    assert.notEqual(runResolver().status, 0);
    await writeFile(path.join(temporary, "compose.owner.yaml"), requestedConfig);
  });

  await t.test("rejects an unscoped configured image", () => {
    assert.notEqual(runResolver({ CONFIGURED_IMAGE: "clickclack:latest" }).status, 0);
  });

  await t.test("rejects a runtime commit without a verified release", () => {
    assert.notEqual(
      runResolver({ CONFIGURED_IMAGE: `clickclack:fakeco-${"e".repeat(40)}` }).status,
      0,
    );
  });

  await t.test("rejects recorded image identity drift", async () => {
    await writeFile(path.join(stateRoot, `image-${running}.id`), `sha256:${"f".repeat(64)}\n`);
    assert.notEqual(runResolver().status, 0);
    await writeFile(path.join(stateRoot, `image-${running}.id`), `${imageID}\n`);
  });

  await t.test("rejects running image identity drift", () => {
    assert.notEqual(runResolver({ RUNNING_IMAGE_ID: `sha256:${"f".repeat(64)}` }).status, 0);
  });

  await t.test("rejects local image identity drift", () => {
    assert.notEqual(runResolver({ LOCAL_IMAGE_ID: `sha256:${"f".repeat(64)}` }).status, 0);
  });

  await t.test("rejects OCI revision drift", () => {
    assert.notEqual(runResolver({ OCI_REVISION: "f".repeat(40) }).status, 0);
  });

  await t.test("rejects an invalid stored release digest", async () => {
    await writeFile(path.join(releaseRoot, running, ".source.sha256"), "invalid\n");
    assert.notEqual(runResolver().status, 0);
    await writeFile(path.join(releaseRoot, running, ".source.sha256"), `${sourceDigest}\n`);
  });

  await t.test("rejects multiple running Compose app containers", () => {
    assert.notEqual(runResolver({ MULTIPLE_CONTAINERS: "true" }).status, 0);
  });

  await t.test("rejects zero running Compose app containers", () => {
    assert.notEqual(runResolver({ NO_CONTAINERS: "true" }).status, 0);
  });
});

test("update bootstrap retains a verified live backup before new code starts", async (t) => {
  const temporary = await temporaryDirectory(t);
  const bootstrap = await readFile(bootstrapPath, "utf8");
  const functionsStart = bootstrap.indexOf("runtime_state_exists() {");
  const functionsEnd = bootstrap.indexOf('\nif [[ "$action" == "bootstrap" ]]', functionsStart);
  assert.notEqual(functionsStart, -1);
  assert.notEqual(functionsEnd, -1);
  const prepareFunctions = bootstrap.slice(functionsStart, functionsEnd).trim();
  const requested = "a".repeat(40);
  const running = "b".repeat(40);
  const tracePath = path.join(temporary, "trace.log");
  const runtimeEnv = path.join(temporary, "runtime.env");
  const runtimeOverride = path.join(temporary, "compose.owner.yaml");
  await writeFile(runtimeEnv, "runtime\n");
  await writeFile(runtimeOverride, "override\n");
  const scriptPath = path.join(temporary, "prepare-update.sh");
  await writeFile(
    scriptPath,
    `#!/usr/bin/env bash
set -euo pipefail
${prepareFunctions}
trace() { printf '%s\n' "$1" >>"$TRACE_PATH"; }
runtime_state_exists() { [[ "\${RUNTIME_STATE:-true}" == true ]]; }
systemctl() { trace systemctl; }
resolve_backup_runtime() {
  trace resolve
  runtime_source_commit="$RUNNING_COMMIT"
  runtime_source_sha256="$RUNNING_SHA256"
  compose_override="$backup_runtime_override"
}
verify_running_image() { trace verify; }
probe_service() { trace probe; }
create_pre_update_backup() {
  trace backup
  [[ "\${FAIL_BACKUP:-false}" != true ]] || return 17
}
rm() { trace cleanup; }
set_runtime_paths() { trace reset; }
stage=initialize
runtime_env="$RUNTIME_ENV"
runtime_override="$RUNTIME_OVERRIDE"
backup_runtime_override="$TEST_ROOT/compose.backup.yaml"
pre_update_backup=false
requested_source_commit="$REQUESTED_COMMIT"
runtime_source_commit="$requested_source_commit"
runtime_source_sha256="$REQUESTED_SHA256"
CLICKCLACK_SOURCE_SHA256="$REQUESTED_SHA256"
compose_override="$runtime_override"
prepare_pre_update_backup
printf 'runtime=%s\npre_update=%s\ncompose=%s\n' \
  "$runtime_source_commit" "$pre_update_backup" "$compose_override"
`,
    { mode: 0o755 },
  );
  const environment = {
    ...process.env,
    TEST_ROOT: temporary,
    TRACE_PATH: tracePath,
    RUNTIME_ENV: runtimeEnv,
    RUNTIME_OVERRIDE: runtimeOverride,
    REQUESTED_COMMIT: requested,
    REQUESTED_SHA256: "c".repeat(64),
    RUNNING_COMMIT: running,
    RUNNING_SHA256: "d".repeat(64),
  };
  const runPrepare = async (overrides = {}) => {
    await writeFile(tracePath, "");
    const result = spawnSync("bash", [scriptPath], {
      encoding: "utf8",
      env: { ...environment, ...overrides },
    });
    const trace = (await readFile(tracePath, "utf8")).trim().split("\n").filter(Boolean);
    return { result, trace };
  };

  const update = await runPrepare();
  assert.equal(update.result.status, 0, update.result.stderr);
  assert.deepEqual(update.trace, [
    "systemctl",
    "resolve",
    "verify",
    "probe",
    "backup",
    "cleanup",
    "reset",
  ]);
  assert.match(update.result.stdout, new RegExp(`runtime=${requested}`, "u"));
  assert.match(update.result.stdout, /pre_update=false/u);
  assert.match(update.result.stdout, new RegExp(`compose=${runtimeOverride}`, "u"));

  const firstApply = await runPrepare({ RUNTIME_STATE: "false" });
  assert.equal(firstApply.result.status, 0, firstApply.result.stderr);
  assert.deepEqual(firstApply.trace, []);

  const sameCommit = await runPrepare({ RUNNING_COMMIT: requested });
  assert.equal(sameCommit.result.status, 0, sameCommit.result.stderr);
  assert.deepEqual(sameCommit.trace, [
    "systemctl",
    "resolve",
    "verify",
    "probe",
    "backup",
    "cleanup",
    "reset",
  ]);

  const failedBackup = await runPrepare({ FAIL_BACKUP: "true" });
  assert.equal(failedBackup.result.status, 17);
  assert.deepEqual(failedBackup.trace, ["systemctl", "resolve", "verify", "probe", "backup"]);
});

test("successful cleanup retains the active release and removes stale build artifacts", async (t) => {
  const temporary = await temporaryDirectory(t);
  const bootstrap = await readFile(bootstrapPath, "utf8");
  const cleanupStart = bootstrap.indexOf("cleanup_success() {");
  const cleanupEnd = bootstrap.indexOf("\ncleanup_run_files() {", cleanupStart);
  assert.notEqual(cleanupStart, -1);
  assert.notEqual(cleanupEnd, -1);
  const cleanupFunction = bootstrap.slice(cleanupStart, cleanupEnd).trim();
  const current = "a".repeat(40);
  const stale = "b".repeat(40);
  const releaseRoot = path.join(temporary, "releases");
  const stateRoot = path.join(temporary, "state");
  const bin = path.join(temporary, "bin");
  const backup = path.join(temporary, "backup.db");
  const dockerLog = path.join(temporary, "docker.log");
  await mkdir(path.join(releaseRoot, current), { recursive: true });
  await mkdir(path.join(releaseRoot, stale), { recursive: true });
  await mkdir(path.join(releaseRoot, "manual"), { recursive: true });
  await mkdir(stateRoot, { recursive: true });
  await mkdir(bin, { recursive: true });
  await writeFile(path.join(stateRoot, `image-${current}.id`), `sha256:${"a".repeat(64)}\n`);
  await writeFile(path.join(stateRoot, `image-${stale}.id`), `sha256:${"b".repeat(64)}\n`);
  await writeFile(backup, "synthetic backup\n");
  await writeFile(
    path.join(bin, "docker"),
    `#!/usr/bin/env bash
set -euo pipefail
printf '%s\\n' "$*" >>"$DOCKER_LOG"
if [[ "\${1:-} \${2:-}" == "image ls" ]]; then
  printf '%s\\n' "clickclack:fakeco-$CURRENT_COMMIT" "clickclack:fakeco-$STALE_COMMIT"
fi
if [[ -n "\${FAIL_DOCKER_MATCH:-}" && "$*" == *"$FAIL_DOCKER_MATCH"* ]]; then
  exit 17
fi
`,
    { mode: 0o755 },
  );
  const scriptPath = path.join(temporary, "cleanup-test.sh");
  await writeFile(
    scriptPath,
    `#!/usr/bin/env bash
set -euo pipefail
${cleanupFunction}
stage=initialize
action="\${TEST_ACTION:-verify}"
release_root="$TEST_ROOT/releases"
state_root="$TEST_ROOT/state"
release="$release_root/$CURRENT_COMMIT"
image_state="$state_root/image-$CURRENT_COMMIT.id"
image_name="clickclack:fakeco-$CURRENT_COMMIT"
cleanup_success "$TEST_ROOT/backup.db"
`,
    { mode: 0o755 },
  );
  const cleanupEnvironment = {
    ...process.env,
    PATH: `${bin}:${process.env.PATH}`,
    TEST_ROOT: temporary,
    CURRENT_COMMIT: current,
    STALE_COMMIT: stale,
    DOCKER_LOG: dockerLog,
  };
  await t.test(
    "normal cleanup retains active state and removes stale build artifacts",
    async () => {
      const result = spawnSync("bash", [scriptPath], {
        encoding: "utf8",
        env: cleanupEnvironment,
      });
      assert.equal(result.status, 0, result.stderr);
      await stat(path.join(releaseRoot, current));
      await stat(path.join(releaseRoot, "manual"));
      await stat(path.join(stateRoot, `image-${current}.id`));
      await assert.rejects(stat(path.join(releaseRoot, stale)), { code: "ENOENT" });
      await assert.rejects(stat(path.join(stateRoot, `image-${stale}.id`)), { code: "ENOENT" });
      await assert.rejects(stat(backup), { code: "ENOENT" });
      const commands = await readFile(dockerLog, "utf8");
      assert.match(commands, /container prune --force/u);
      assert.match(commands, new RegExp(`image rm clickclack:fakeco-${stale}`, "u"));
      assert.doesNotMatch(commands, new RegExp(`image rm clickclack:fakeco-${current}`, "u"));
      assert.match(commands, /image prune --force/u);
      assert.match(commands, /builder prune --force/u);
    },
  );

  await t.test("failed normal cleanup preserves the local backup", async () => {
    await writeFile(backup, "failure-retained backup\n");
    const failedCleanup = spawnSync("bash", [scriptPath], {
      encoding: "utf8",
      env: { ...cleanupEnvironment, FAIL_DOCKER_MATCH: "builder prune" },
    });
    assert.equal(failedCleanup.status, 17);
    await stat(backup);
  });

  await t.test(
    "teardown removes only the local backup and preserves old and new release and image state",
    async () => {
      await mkdir(path.join(releaseRoot, stale), { recursive: true });
      await writeFile(path.join(stateRoot, `image-${stale}.id`), `sha256:${"b".repeat(64)}\n`);
      await writeFile(backup, "teardown backup\n");
      const dockerCommandsBeforeBackup = await readFile(dockerLog, "utf8");
      const backupCleanup = spawnSync("bash", [scriptPath], {
        encoding: "utf8",
        env: { ...cleanupEnvironment, TEST_ACTION: "backup" },
      });
      assert.equal(backupCleanup.status, 0, backupCleanup.stderr);
      await stat(path.join(releaseRoot, current));
      await stat(path.join(releaseRoot, stale));
      await stat(path.join(stateRoot, `image-${current}.id`));
      await stat(path.join(stateRoot, `image-${stale}.id`));
      await assert.rejects(stat(backup), { code: "ENOENT" });
      assert.equal(await readFile(dockerLog, "utf8"), dockerCommandsBeforeBackup);
    },
  );
});

test("bootstrap proves seed equality, health, readiness, metadata metrics, and backup integrity", async () => {
  const bootstrap = await readFile(bootstrapPath, "utf8");
  const runbook = await readFile(runbookPath, "utf8");
  assert.doesNotMatch(bootstrap, /^\s*awscli\s*\\\s*$/mu);
  assert.match(bootstrap, /readonly aws_cli_version=2\.35\.20/u);
  assert.match(
    bootstrap,
    /readonly aws_cli_archive_sha256=58799ce9276d4e8815fd19e4dc35649626c6b4fbd4d0e3df7433af9cfde41882/u,
  );
  assert.match(bootstrap, /awscli-exe-linux-aarch64-\$aws_cli_version\.zip/u);
  assert.match(bootstrap, /dpkg --print-architecture \| grep -Fx arm64/u);
  assert.match(bootstrap, /sha256sum --check --status/u);
  assert.match(bootstrap, /unzip -q "\$archive"/u);
  assert.match(bootstrap, /\/usr\/local\/bin\/aws --version/u);
  assert.match(bootstrap, /docker version --format '\{\{\.Server\.Arch\}\}' \| grep -Fx 'arm64'/u);
  assert.match(bootstrap, /clickclack:fakeco-\$runtime_source_commit/u);
  assert.match(bootstrap, /org\.opencontainers\.image\.revision/u);
  assert.match(bootstrap, /\.source\.sha256/u);
  assert.match(bootstrap, /docker inspect --format '\{\{\.Image\}\}'/u);
  assert.match(bootstrap, /docker inspect --format '\{\{\.Config\.Image\}\}'/u);
  assert.equal((bootstrap.match(/--profile tools run --rm seed/g) ?? []).length, 2);
  assert.match(bootstrap, /cmp -s/u);
  assert.match(bootstrap, /healthz/u);
  assert.match(bootstrap, /readyz/u);
  assert.match(bootstrap, /clickclack_ready 1/u);
  assert.match(bootstrap, /seed_manifest_sha256/u);
  assert.match(bootstrap, /metrics contained forbidden high-cardinality or body content/u);
  assert.match(bootstrap, /clickclack backup/u);
  assert.match(bootstrap, /PRAGMA integrity_check/u);
  assert.match(bootstrap, /--sse aws:kms/u);
  assert.match(
    bootstrap,
    /--metadata "sha256=\$captured_backup_sha,source-commit=\$runtime_source_commit"/u,
  );
  assert.match(bootstrap, /stack_source_commit: \$stack_source_commit/u);
  assert.match(bootstrap, /resolve_backup_runtime/u);
  assert.match(bootstrap, /label=com\.docker\.compose\.project=clickclack-fakeco/u);
  assert.match(bootstrap, /label=com\.docker\.compose\.service=app/u);
  assert.match(bootstrap, /compose\.backup\.yaml/u);
  assert.ok(
    bootstrap.lastIndexOf("resolve_backup_runtime") < bootstrap.lastIndexOf("verify_running_image"),
    "backup must resolve the running release before identity verification",
  );
  assert.match(bootstrap, /aws s3api head-object/u);
  assert.match(bootstrap, /\.ContentLength == \$size/u);
  assert.match(bootstrap, /cleanup_success "\$captured_backup_path"/u);
  assert.match(bootstrap, /rm -f -- "\$backup_path"/u);
  assert.match(bootstrap, /reference=clickclack:fakeco-\*/u);
  assert.match(bootstrap, /docker image prune --force/u);
  assert.match(bootstrap, /docker builder prune --force/u);
  assert.match(bootstrap, /\^\[0-9a-f\]\{40\}\$/u);
  assert.match(bootstrap, /\^image-\[0-9a-f\]\{40\}\\\.id\$/u);
  assert.ok(
    bootstrap.lastIndexOf('--body "$evidence_file"') <
      bootstrap.lastIndexOf('cleanup_success "$captured_backup_path"'),
    "cleanup must wait for durable backup evidence",
  );
  assert.ok(
    bootstrap.lastIndexOf("aws s3api head-object") <
      bootstrap.lastIndexOf('cleanup_success "$captured_backup_path"'),
    "cleanup must wait for remote backup metadata verification",
  );
  const dispatchStart = bootstrap.lastIndexOf('if [[ "$action" == "bootstrap" ]]');
  const prepareCall = bootstrap.indexOf("  prepare_pre_update_backup", dispatchStart);
  const installCall = bootstrap.indexOf("  install_runtime", dispatchStart);
  const configCall = bootstrap.indexOf("  write_runtime_config", dispatchStart);
  const startCall = bootstrap.indexOf("  build_and_start", dispatchStart);
  assert.ok(
    dispatchStart >= 0 &&
      prepareCall > dispatchStart &&
      prepareCall < installCall &&
      installCall < configCall &&
      configCall < startCall,
    "a verified pre-update backup must finish before runtime config or new code starts",
  );
  assert.match(bootstrap, /--arg action pre-update/u);
  assert.match(bootstrap, /requested_source_commit: \$requested_source_commit/u);
  assert.match(bootstrap, /pre-update-upload-evidence/u);
  assert.match(bootstrap, /printf 'pre-update:%s:%s:%s'/u);
  assert.match(bootstrap, /backup_id="\$\{run_id%-\*\}-\$backup_suffix"/u);
  assert.match(bootstrap, /\[\[ "\$backup_id" != "\$run_id" \]\]/u);
  assert.match(bootstrap, /readonly max_single_put_bytes=5000000000/u);
  assert.match(bootstrap, /captured_backup_size > max_single_put_bytes/u);
  assert.match(runbook, /backup objects up to\s+5,000,000,000 bytes/u);
  assert.equal(
    (bootstrap.match(/--if-none-match '\*'/gu) ?? []).length,
    3,
    "backup databases and both evidence objects must be create-only",
  );
  assert.doesNotMatch(bootstrap, /docker (system|volume) prune/u);
  assert.doesNotMatch(
    bootstrap,
    /CLICKCLACK_TOKEN|CLAWROUTER_API_KEY|OPENCLAW_GATEWAY_TOKEN|down --volumes/u,
  );
  assert.match(runbook, /runtime_override=\/etc\/clickclack-fakeco\/compose\.owner\.yaml/u);
  assert.match(runbook, /\(\nset -euo pipefail\numask 077/u);
  assert.match(runbook, /runtime_commit="\$\(jq -er '\.source_commit/u);
  assert.match(runbook, /backup_bucket="\$\(jq -er '\.backup\.bucket/u);
  assert.match(runbook, /backup_key="\$\(jq -er '\.backup\.key/u);
  assert.match(runbook, /backup_sha256="\$\(jq -er '\.backup\.sha256/u);
  assert.match(runbook, /sqlite\/\$runtime_commit\/clickclack-/u);
  assert.match(runbook, /release="\/opt\/clickclack\/releases\/\$runtime_commit"/u);
  assert.match(runbook, /clickclack:fakeco-\$runtime_commit/u);
  assert.match(runbook, /-f "\$runtime_override"/u);
  assert.match(runbook, /s3:\/\/\$backup_bucket\/\$backup_key/u);
  assert.match(runbook, /"\$backup_sha256" "\$restore_dir\/clickclack\.db"/u);
  assert.match(runbook, /wait_ready\(\)/u);
  assert.match(runbook, /for _ in \$\(seq 1 60\)/u);
  assert.match(runbook, /trap rollback_restore ERR/u);
  assert.match(runbook, /cp -a -- "\$restore_dir\/previous\/\$file"/u);
  assert.match(runbook, /wait_ready \|\| "\$\{compose\[@\]\}" stop app/u);
});

function fakecoEnvironment() {
  return {
    FAKECO_AWS_ACCOUNT_ID: "123456789012",
    FAKECO_AWS_REGION: "us-west-2",
    FAKECO_GITHUB_ROLE_ARN:
      "arn:aws:iam::123456789012:role/openclaw/fakeco/github/clickclack-owner",
    FAKECO_CLOUDFORMATION_SERVICE_ROLE_ARN:
      "arn:aws:iam::123456789012:role/openclaw/fakeco/cloudformation/clickclack-service",
    FAKECO_WORKLOAD_PERMISSIONS_BOUNDARY_ARN:
      "arn:aws:iam::123456789012:policy/openclaw/fakeco/clickclack-workload-boundary",
    FAKECO_VPC_ID: "vpc-1234abcd",
    FAKECO_PRIVATE_SUBNET_ID: "subnet-1234abcd",
    FAKECO_EGRESS_RESOURCE_ID: "nat-1234abcd",
    FAKECO_OPENCLAW_GATEWAY_SECURITY_GROUP_ID: "sg-1234abcd",
    FAKECO_METRICS_SECURITY_GROUP_ID: "sg-abcd1234",
    FAKECO_AMI_ID: "ami-1234abcd",
    FAKECO_ARTIFACT_BUCKET: "openclaw-fakeco-artifact-123456789012",
    FAKECO_ARTIFACT_PREFIX: "clickclack/fakeco/artifacts",
    FAKECO_LOG_BUCKET: "openclaw-fakeco-logs-123456789012",
    FAKECO_LOG_PREFIX: "clickclack/fakeco/logs",
    FAKECO_BACKUP_BUCKET: "openclaw-fakeco-backups-123456789012",
    FAKECO_BACKUP_PREFIX: "clickclack/fakeco/backups",
    FAKECO_DATA_KMS_KEY_ARN: kmsKeyArn,
  };
}

function stackResponse(rendered) {
  return {
    Stacks: [
      {
        StackName: "clickclack-fakeco",
        StackStatus: "UPDATE_COMPLETE",
        EnableTerminationProtection: true,
        RoleARN: rendered.target.cloudFormationServiceRoleArn,
        Parameters: rendered.parameters.map((entry) => ({ ...entry })),
        Tags: [
          ...rendered.tags.map((entry) => ({ ...entry })),
          ...cloudFormationTags("clickclack-fakeco"),
        ],
        Outputs: [
          { OutputKey: "InstanceId", OutputValue: "i-1234abcd" },
          { OutputKey: "PrivateIp", OutputValue: "10.0.1.20" },
          { OutputKey: "SecurityGroupId", OutputValue: "sg-fedcba98" },
          {
            OutputKey: "InstanceProfileArn",
            OutputValue:
              "arn:aws:iam::123456789012:instance-profile/openclaw/fakeco/clickclack/clickclack-fakeco-InstanceProfile-ABCDEFG",
          },
          {
            OutputKey: "InstanceProfileName",
            OutputValue: "clickclack-fakeco-InstanceProfile-ABCDEFG",
          },
          {
            OutputKey: "InstanceRoleArn",
            OutputValue:
              "arn:aws:iam::123456789012:role/openclaw/fakeco/clickclack/clickclack-fakeco-InstanceRole-ABCDEFG",
          },
          {
            OutputKey: "InstanceRoleName",
            OutputValue: "clickclack-fakeco-InstanceRole-ABCDEFG",
          },
          { OutputKey: "SourceCommit", OutputValue: rendered.source.commit },
          { OutputKey: "VpcId", OutputValue: rendered.target.vpcId },
          { OutputKey: "PrivateSubnetId", OutputValue: rendered.target.subnetId },
        ],
      },
    ],
  };
}

async function stackInventoryArgs(temporary, rendered) {
  const observedTemplatePath = path.join(temporary, "observed-template.json");
  const resourcesPath = path.join(temporary, "observed-resources.json");
  await writeFile(observedTemplatePath, JSON.stringify(ownerTemplate));
  await writeFile(resourcesPath, JSON.stringify(stackResourcesResponse(rendered)));
  return ["--template", observedTemplatePath, "--resources", resourcesPath];
}

function stackResourcesResponse(rendered) {
  const resources = Object.entries(ownerTemplate.Resources).filter(([, resource]) => {
    return (
      resource.Condition !== "AllowMetricsSource" || rendered.target.metricsSecurityGroupId !== ""
    );
  });
  return {
    StackResources: resources.map(([logicalID, resource], index) => ({
      StackName: "clickclack-fakeco",
      LogicalResourceId: logicalID,
      PhysicalResourceId: `physical-${index}`,
      ResourceType: resource.Type,
      ResourceStatus: "CREATE_COMPLETE",
    })),
  };
}

function instanceResponse() {
  return {
    Reservations: [
      {
        Instances: [
          {
            InstanceId: "i-1234abcd",
            InstanceType: "t4g.small",
            Architecture: "arm64",
            ImageId: "ami-1234abcd",
            VpcId: "vpc-1234abcd",
            SubnetId: "subnet-1234abcd",
            PrivateIpAddress: "10.0.1.20",
            State: { Name: "running" },
            MetadataOptions: { HttpTokens: "required", HttpPutResponseHopLimit: 1 },
            SecurityGroups: [{ GroupId: "sg-fedcba98", GroupName: "fakeco" }],
            IamInstanceProfile: {
              Arn: "arn:aws:iam::123456789012:instance-profile/openclaw/fakeco/clickclack/clickclack-fakeco-InstanceProfile-ABCDEFG",
            },
            BlockDeviceMappings: [
              {
                DeviceName: "/dev/sda1",
                Ebs: { VolumeId: "vol-1234abcd", DeleteOnTermination: false },
              },
            ],
          },
        ],
      },
    ],
  };
}

function volumeResponse() {
  return {
    Volumes: [
      {
        VolumeId: "vol-1234abcd",
        AvailabilityZone: "us-west-2a",
        Size: 16,
        VolumeType: "gp3",
        Encrypted: true,
        KmsKeyId: kmsKeyArn,
        State: "in-use",
      },
    ],
  };
}

function securityGroupResponse(rendered) {
  return {
    SecurityGroups: [
      {
        GroupId: "sg-fedcba98",
        VpcId: rendered.target.vpcId,
        IpPermissions: [
          {
            IpProtocol: "tcp",
            FromPort: 8080,
            ToPort: 8080,
            IpRanges: [],
            Ipv6Ranges: [],
            PrefixListIds: [],
            UserIdGroupPairs: [
              {
                UserId: rendered.target.accountId,
                GroupId: rendered.target.gatewaySecurityGroupId,
              },
              {
                UserId: rendered.target.accountId,
                GroupId: rendered.target.metricsSecurityGroupId,
              },
            ],
          },
        ],
      },
    ],
  };
}

function workloadIAMResponse(rendered) {
  const roleName = "clickclack-fakeco-InstanceRole-ABCDEFG";
  const profileName = "clickclack-fakeco-InstanceProfile-ABCDEFG";
  const roleArn = `arn:aws:iam::${rendered.target.accountId}:role/openclaw/fakeco/clickclack/${roleName}`;
  const profileArn = `arn:aws:iam::${rendered.target.accountId}:instance-profile/openclaw/fakeco/clickclack/${profileName}`;
  return {
    InstanceProfile: {
      Path: "/openclaw/fakeco/clickclack/",
      InstanceProfileName: profileName,
      Arn: profileArn,
      Roles: [{ Path: "/openclaw/fakeco/clickclack/", RoleName: roleName, Arn: roleArn }],
    },
    Role: {
      Path: "/openclaw/fakeco/clickclack/",
      RoleName: roleName,
      Arn: roleArn,
      AssumeRolePolicyDocument:
        ownerTemplate.Resources.InstanceRole.Properties.AssumeRolePolicyDocument,
      PermissionsBoundary: {
        PermissionsBoundaryType: "Policy",
        PermissionsBoundaryArn: rendered.target.permissionsBoundaryArn,
      },
      Tags: [
        ...rendered.tags.map((entry) => ({ ...entry })),
        ...cloudFormationTags("InstanceRole"),
      ],
    },
    AttachedPolicies: [],
    PolicyNames: ownerTemplate.Resources.InstanceRole.Properties.Policies.map(
      (policy) => policy.PolicyName,
    ),
    RolePolicies: ownerTemplate.Resources.InstanceRole.Properties.Policies.map((policy) => ({
      RoleName: roleName,
      PolicyName: policy.PolicyName,
      PolicyDocument: resolvePolicyParameters(policy.PolicyDocument, rendered),
    })),
  };
}

function cloudFormationTags(logicalId) {
  return [
    { Key: "aws:cloudformation:logical-id", Value: logicalId },
    {
      Key: "aws:cloudformation:stack-id",
      Value:
        "arn:aws:cloudformation:us-west-2:123456789012:stack/clickclack-fakeco/12345678-1234-1234-1234-123456789abc",
    },
    { Key: "aws:cloudformation:stack-name", Value: "clickclack-fakeco" },
  ];
}

function resolvePolicyParameters(value, rendered) {
  if (Array.isArray(value)) {
    return value.map((entry) => resolvePolicyParameters(entry, rendered));
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  const parameters = new Map(
    rendered.parameters.map((entry) => [entry.ParameterKey, entry.ParameterValue]),
  );
  if (Object.keys(value).length === 1 && typeof value.Ref === "string") {
    return parameters.get(value.Ref);
  }
  if (Object.keys(value).length === 1 && typeof value["Fn::Sub"] === "string") {
    return value["Fn::Sub"].replace(/\$\{([^}]+)\}/gu, (_match, name) => parameters.get(name));
  }
  return Object.fromEntries(
    Object.entries(value).map(([key, entry]) => [key, resolvePolicyParameters(entry, rendered)]),
  );
}

function snapshotResponse() {
  return {
    Snapshots: [
      {
        SnapshotId: "snap-1234abcd",
        VolumeId: "vol-1234abcd",
        State: "completed",
        Encrypted: true,
        KmsKeyId: kmsKeyArn,
      },
    ],
  };
}

function backupEvidence(rendered, runtimeCommit = rendered.source.commit) {
  const action = { apply: "bootstrap", verify: "verify", teardown: "backup" }[rendered.phase];
  return {
    schema_version: 1,
    status: "passed",
    action,
    source_commit: runtimeCommit,
    stack_source_commit: rendered.source.commit,
    owner_commit: rendered.source.ownerCommit,
    runtime_commit_verified: true,
    image_id: `sha256:${"c".repeat(64)}`,
    seed_equal: true,
    seed_manifest_sha256: "d".repeat(64),
    health: true,
    readiness: true,
    metrics_metadata_only: true,
    integrity_check: "ok",
    backup: {
      bucket: rendered.destinations.backups.bucket,
      key: `${rendered.destinations.backups.prefix}/sqlite/${runtimeCommit}/clickclack.db`,
      sha256: "b".repeat(64),
    },
    manifest: {
      bucket: rendered.destinations.backups.bucket,
      key: `${rendered.destinations.backups.prefix}/manifests/test.json`,
    },
  };
}

function runOwner(args, environment = {}) {
  return spawnSync(process.execPath, [ownerPath, ...args], {
    cwd: repositoryRoot,
    encoding: "utf8",
    env: { ...process.env, ...environment },
  });
}

async function temporaryDirectory(t) {
  const temporary = await mkdtemp(path.join(os.tmpdir(), "clickclack-fakeco-owner-"));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  return temporary;
}
