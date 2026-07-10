import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
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
  await writeFile(stackPath, JSON.stringify(stackResponse(rendered)));
  await writeFile(instancePath, JSON.stringify(instanceResponse()));
  await writeFile(volumePath, JSON.stringify(volumeResponse()));
  await writeFile(securityGroupPath, JSON.stringify(securityGroupResponse(rendered)));
  await writeFile(workloadIAMPath, JSON.stringify(workloadIAMResponse(rendered)));
  await writeFile(evidencePath, JSON.stringify(backupEvidence(rendered)));
  await writeFile(snapshotPath, JSON.stringify(snapshotResponse()));

  assert.equal(
    runOwner(["verify-stack", "--rendered", renderedPath, "--stack", stackPath]).status,
    0,
  );
  assert.equal(
    runOwner([
      "verify-instance",
      "--rendered",
      renderedPath,
      "--stack",
      stackPath,
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
  await writeFile(evidencePath, JSON.stringify(backupEvidence(rendered)));
  const retention = runOwner([
    "retention-manifest",
    "--rendered",
    renderedPath,
    "--stack",
    stackPath,
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
  await writeFile(stackPath, JSON.stringify(stack));
  const result = runOwner(["verify-stack", "--rendered", renderedPath, "--stack", stackPath]);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /parameter VpcId drifted/u);
  assert.doesNotMatch(result.stderr, /vpc-private-drift/u);
  assert.doesNotMatch(result.stderr, /vpc-1234abcd/u);
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
  await writeFile(stackPath, JSON.stringify(stackResponse(rendered)));
  await writeFile(volumePath, JSON.stringify(volumeResponse()));
  await writeFile(securityGroupPath, JSON.stringify(securityGroupResponse(rendered)));

  const cases = [
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
  assert.ok(
    workflow.indexOf("create-change-set") < workflow.indexOf("execute-change-set"),
    "change set must be created before it can execute",
  );
  assert.match(workflow, /seq 1 60/u);
  assert.match(workflow, /seq 1 240/u);
  assert.match(workflow, /executionTimeout: \["2400"\]/u);
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
  assert.match(workflow, /destroy-clickclack-fakeco-retain-data/u);
  assert.match(workflow, /deletion-mode STANDARD/u);
  assert.doesNotMatch(
    workflow,
    /delete-snapshot|delete-volume|delete-object|s3 rm|down --volumes/u,
  );
});

test("bootstrap proves seed equality, health, readiness, metadata metrics, and backup integrity", async () => {
  const bootstrap = await readFile(bootstrapPath, "utf8");
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
  assert.match(bootstrap, /clickclack:fakeco-\$CLICKCLACK_SOURCE_COMMIT/u);
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
  assert.doesNotMatch(
    bootstrap,
    /CLICKCLACK_TOKEN|CLAWROUTER_API_KEY|OPENCLAW_GATEWAY_TOKEN|down --volumes/u,
  );
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

function backupEvidence(rendered) {
  return {
    schema_version: 1,
    status: "passed",
    source_commit: rendered.source.commit,
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
      key: `${rendered.destinations.backups.prefix}/sqlite/${sourceCommit}/clickclack.db`,
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
