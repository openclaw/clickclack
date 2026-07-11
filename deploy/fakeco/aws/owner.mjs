#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const directory = path.dirname(fileURLToPath(import.meta.url));
const profilePath = path.join(directory, "profile.json");
const templatePath = path.join(directory, "template.json");
const profile = JSON.parse(await readFile(profilePath, "utf8"));
const template = JSON.parse(await readFile(templatePath, "utf8"));

const command = process.argv[2] ?? "";
const options = parseOptions(process.argv.slice(3));

try {
  switch (command) {
    case "validate-profile":
      validateProfile();
      print({ ok: true, stackName: profile.stackName, region: profile.region });
      break;
    case "render":
      await render();
      break;
    case "parameters":
      await writeParameters();
      break;
    case "verify-stack":
      await verifyStackCommand();
      break;
    case "verify-instance":
      await verifyInstanceCommand();
      break;
    case "verify-backup":
      await verifyBackupCommand();
      break;
    case "verify-kms-policy":
      await verifyKMSPolicyCommand();
      break;
    case "retention-manifest":
      await retentionManifest();
      break;
    default:
      throw new Error(
        "usage: owner.mjs <validate-profile|render|parameters|verify-stack|verify-instance|verify-backup|verify-kms-policy|retention-manifest>",
      );
  }
} catch (error) {
  console.error(`fakeco AWS owner: ${error instanceof Error ? error.message : String(error)}`);
  process.exitCode = 1;
}

function validateProfile() {
  assert(profile.schemaVersion === 1, "profile schema version must be 1");
  assert(profile.name === "fakeco", "profile name must be fakeco");
  assert(profile.repository === "openclaw/clickclack", "profile repository drifted");
  assert(profile.environment === "fakeco", "GitHub Environment must be fakeco");
  assert(profile.region === "us-west-2", "region must be us-west-2");
  assert(profile.stackName === "clickclack-fakeco", "stack name drifted");
  validateCommit(profile.defaultCommit, "default commit");
  assert(profile.instance.type === "t4g.small", "instance type must be t4g.small");
  assert(profile.instance.architecture === "arm64", "instance architecture must be arm64");
  assert(profile.instance.rootVolumeGiB === 16, "root volume must be 16 GiB");
  assert(profile.instance.rootVolumeType === "gp3", "root volume must be gp3");
  assert(profile.instance.rootDeviceName === "/dev/sda1", "root device name drifted");

  const resources = template.Resources ?? {};
  const resourceTypes = Object.values(resources).map((resource) => resource.Type);
  const forbidden = new Set([
    "AWS::EC2::VPC",
    "AWS::EC2::Subnet",
    "AWS::EC2::NatGateway",
    "AWS::EC2::EIP",
    "AWS::EC2::InternetGateway",
    "AWS::EC2::Route",
    "AWS::EC2::RouteTable",
  ]);
  for (const type of resourceTypes) {
    assert(!forbidden.has(type), `template must not create network resource ${type}`);
  }
  assert(
    resourceTypes.filter((type) => type === "AWS::EC2::Instance").length === 1,
    "template must own exactly one EC2 instance",
  );

  const instance = resources.Instance?.Properties;
  assert(instance?.InstanceType === profile.instance.type, "template instance type drifted");
  assert(
    instance?.CreditSpecification?.CPUCredits === "standard",
    "T4g CPU credits must be cost-bounded",
  );
  assert(instance?.KeyName === undefined, "template must not configure an SSH key");
  assert(instance?.MetadataOptions?.HttpTokens === "required", "IMDSv2 must be required");
  assert(instance?.MetadataOptions?.HttpPutResponseHopLimit === 1, "IMDS hop limit must be one");
  assert(instance?.NetworkInterfaces?.length === 1, "instance must have one network interface");
  assert(
    instance.NetworkInterfaces[0].AssociatePublicIpAddress === false,
    "instance must not receive a public IP",
  );
  const root = instance.BlockDeviceMappings?.find(
    (mapping) => mapping.DeviceName === profile.instance.rootDeviceName,
  )?.Ebs;
  assert(root?.VolumeSize === 16 && root?.VolumeType === "gp3", "root volume shape drifted");
  assert(root?.Encrypted === true, "root volume must be encrypted");
  assert(root?.DeleteOnTermination === false, "root volume must survive termination");

  const ingress = [resources.GatewayIngress, resources.MetricsIngress];
  for (const rule of ingress) {
    assert(
      rule?.Type === "AWS::EC2::SecurityGroupIngress",
      "only explicit SG ingress resources are allowed",
    );
    assert(rule.Properties?.IpProtocol === "tcp", "FakeCo ingress must use TCP");
    assert(
      rule.Properties?.FromPort === 8080 && rule.Properties?.ToPort === 8080,
      "FakeCo ingress must be TCP 8080 only",
    );
    assert(
      rule.Properties?.CidrIp === undefined && rule.Properties?.CidrIpv6 === undefined,
      "FakeCo ingress must use a source security group",
    );
    assert(
      rule.Properties?.SourceSecurityGroupId?.Ref,
      "FakeCo ingress source must be an explicit security group parameter",
    );
  }

  const role = resources.InstanceRole?.Properties;
  assert(role?.ManagedPolicyArns === undefined, "instance role must use reviewed inline policies");
  assert(
    role?.PermissionsBoundary?.Ref === "WorkloadPermissionsBoundaryArn",
    "instance role must use the supplied boundary",
  );
  const policies = role?.Policies ?? [];
  assert(policies.length === 2, "instance role policy set drifted");
  const objectPolicy = policies.find((entry) => entry.PolicyName === "ExactFakeCoObjects");
  const statements = objectPolicy?.PolicyDocument?.Statement ?? [];
  assert(statements.length === 5, "exact object policy statement set drifted");
  const listRemoteScriptPrefix = statements.find((entry) => entry.Sid === "ListRemoteScriptPrefix");
  assert(
    canonicalJSON(listRemoteScriptPrefix?.Action) === canonicalJSON(["s3:ListBucket"]) &&
      listRemoteScriptPrefix?.Resource?.Ref === "ArtifactBucketArn" &&
      listRemoteScriptPrefix?.Condition?.StringLike?.["s3:prefix"]?.["Fn::Sub"] ===
        "${ArtifactPrefix}/owner/*",
    "remote script listing must be limited to the artifact owner prefix",
  );
  for (const statement of statements.filter(
    (entry) =>
      entry.Sid !== "ListRemoteScriptPrefix" &&
      entry.Action.some((action) => action.startsWith("s3:")),
  )) {
    const resource = statement.Resource?.["Fn::Sub"] ?? "";
    assert(
      resource.endsWith("/*") && !resource.includes(":*"),
      "S3 permissions must be prefix-scoped",
    );
  }
}

async function render() {
  validateProfile();
  const phase = requiredOption("phase");
  assert(
    ["plan", "apply", "verify", "teardown-plan", "teardown"].includes(phase),
    "phase must be plan, apply, verify, teardown-plan, or teardown",
  );
  const sourceCommit = options.commit ?? profile.defaultCommit;
  const ownerCommit = requiredOption("owner-commit");
  validateCommit(sourceCommit, "source commit");
  validateCommit(ownerCommit, "owner commit");

  const accountId = requiredEnv("FAKECO_AWS_ACCOUNT_ID");
  assert(/^[0-9]{12}$/u.test(accountId), "FAKECO_AWS_ACCOUNT_ID must be 12 digits");
  const region = requiredEnv("FAKECO_AWS_REGION");
  assert(region === profile.region, `FAKECO_AWS_REGION must equal ${profile.region}`);
  const githubRoleArn = exactRoleArn(
    requiredEnv("FAKECO_GITHUB_ROLE_ARN"),
    accountId,
    profile.githubRolePath,
    "GitHub role",
  );
  const cloudFormationServiceRoleArn = exactRoleArn(
    requiredEnv("FAKECO_CLOUDFORMATION_SERVICE_ROLE_ARN"),
    accountId,
    profile.cloudFormationServiceRolePath,
    "CloudFormation service role",
  );
  const permissionsBoundaryArn = requiredEnv("FAKECO_WORKLOAD_PERMISSIONS_BOUNDARY_ARN");
  assert(
    permissionsBoundaryArn ===
      `arn:aws:iam::${accountId}:policy/${profile.workloadPermissionsBoundaryPath}`,
    "workload permissions boundary ARN does not match the locked FakeCo path",
  );

  const vpcId = validateId(requiredEnv("FAKECO_VPC_ID"), /^vpc-[0-9a-f]+$/u, "VPC ID");
  const subnetId = validateId(
    requiredEnv("FAKECO_PRIVATE_SUBNET_ID"),
    /^subnet-[0-9a-f]+$/u,
    "private subnet ID",
  );
  const egressResourceId = validateId(
    requiredEnv("FAKECO_EGRESS_RESOURCE_ID"),
    /^(nat|tgw)-[0-9a-f]+$/u,
    "egress resource ID",
  );
  const gatewaySecurityGroupId = validateId(
    requiredEnv("FAKECO_OPENCLAW_GATEWAY_SECURITY_GROUP_ID"),
    /^sg-[0-9a-f]+$/u,
    "OpenClaw gateway security group ID",
  );
  const metricsSecurityGroupId = optionalEnv("FAKECO_METRICS_SECURITY_GROUP_ID");
  if (metricsSecurityGroupId !== "") {
    validateId(metricsSecurityGroupId, /^sg-[0-9a-f]+$/u, "metrics security group ID");
    assert(
      metricsSecurityGroupId !== gatewaySecurityGroupId,
      "metrics security group must differ from the OpenClaw gateway security group",
    );
  }
  const imageId = validateId(requiredEnv("FAKECO_AMI_ID"), /^ami-[0-9a-f]+$/u, "AMI ID");

  const artifact = destination("ARTIFACT");
  const logs = destination("LOG");
  const backups = destination("BACKUP");
  assertDestinationsDoNotOverlap([
    ["artifact", artifact],
    ["log", logs],
    ["backup", backups],
  ]);
  const kmsKeyArn = requiredEnv("FAKECO_DATA_KMS_KEY_ARN");
  assert(
    new RegExp(`^arn:aws:kms:${profile.region}:${accountId}:key/[0-9a-f-]{36}$`, "u").test(
      kmsKeyArn,
    ),
    "data KMS key must be an exact key ARN in the target account and region",
  );

  const rendered = {
    schemaVersion: 1,
    phase,
    stackName: profile.stackName,
    target: {
      accountId,
      region,
      githubRoleArn,
      cloudFormationServiceRoleArn,
      permissionsBoundaryArn,
      vpcId,
      subnetId,
      egressResourceId,
      gatewaySecurityGroupId,
      metricsSecurityGroupId,
      imageId,
    },
    source: {
      commit: sourceCommit,
      ownerCommit,
      artifactKey: `${artifact.prefix}/${sourceCommit}/${profile.artifactNames.source}`,
      bootstrapKey: `${artifact.prefix}/owner/${ownerCommit}/${profile.artifactNames.bootstrap}`,
    },
    destinations: { artifact, logs, backups, kmsKeyArn },
    parameters: [
      parameter("SourceCommit", sourceCommit),
      parameter("ImageId", imageId),
      parameter("VpcId", vpcId),
      parameter("PrivateSubnetId", subnetId),
      parameter("OpenClawGatewaySecurityGroupId", gatewaySecurityGroupId),
      parameter("MetricsSecurityGroupId", metricsSecurityGroupId),
      parameter("ArtifactBucketArn", artifact.arn),
      parameter("ArtifactPrefix", artifact.prefix),
      parameter("LogBucketArn", logs.arn),
      parameter("LogPrefix", logs.prefix),
      parameter("BackupBucketArn", backups.arn),
      parameter("BackupPrefix", backups.prefix),
      parameter("DataKmsKeyArn", kmsKeyArn),
      parameter("WorkloadPermissionsBoundaryArn", permissionsBoundaryArn),
    ],
    tags: Object.entries(profile.tags).map(([Key, Value]) => ({ Key, Value })),
  };
  await writeJSON(requiredOption("output"), rendered);
  print({ ok: true, phase, stackName: profile.stackName, sourceCommit });
}

async function writeParameters() {
  const rendered = await readRendered(requiredOption("rendered"));
  await writeJSON(requiredOption("output"), rendered.parameters);
  print({ ok: true, parameterCount: rendered.parameters.length });
}

async function verifyKMSPolicyCommand() {
  const accountId = requiredOption("account-id");
  assert(/^[0-9]{12}$/u.test(accountId), "--account-id must be 12 digits");
  const policy = await readJSON(requiredOption("policy"));
  const accountRoot = `arn:aws:iam::${accountId}:root`;
  const delegatedActions = scalarList(policy.Statement)
    .filter(
      (statement) =>
        statement?.Effect === "Allow" &&
        scalarList(statement?.Principal?.AWS).includes(accountRoot) &&
        scalarList(statement?.Resource).includes("*") &&
        statement?.Condition === undefined,
    )
    .flatMap((statement) => scalarList(statement.Action));
  for (const action of [
    "kms:CreateGrant",
    "kms:Decrypt",
    "kms:DescribeKey",
    "kms:Encrypt",
    "kms:GenerateDataKey",
    "kms:GenerateDataKeyWithoutPlaintext",
    "kms:GetKeyPolicy",
    "kms:ReEncryptFrom",
    "kms:ReEncryptTo",
  ]) {
    assert(
      delegatedActions.some((candidate) => actionPatternAllows(candidate, action)),
      "KMS key policy must delegate required actions to target-account IAM policies",
    );
  }
  print({ ok: true, accountId });
}

async function verifyStackCommand() {
  const rendered = await readRendered(requiredOption("rendered"));
  const stack = await verifyObservedStack(rendered);
  print({ ok: true, stackName: rendered.stackName, status: stack.StackStatus });
}

async function verifyInstanceCommand() {
  const rendered = await readRendered(requiredOption("rendered"));
  const stack = await verifyObservedStack(rendered);
  const instanceResponse = await readJSON(requiredOption("instance"));
  const volumeResponse = await readJSON(requiredOption("volume"));
  const securityGroupResponse = await readJSON(requiredOption("security-group"));
  const workloadIAMResponse = await readJSON(requiredOption("workload-iam"));
  verifyInstance(
    rendered,
    stack,
    instanceResponse,
    volumeResponse,
    securityGroupResponse,
    workloadIAMResponse,
  );
  print({
    ok: true,
    instance: "verified",
    volume: "verified",
    securityGroup: "verified",
    workloadIAM: "verified",
  });
}

async function verifyBackupCommand() {
  const rendered = await readRendered(requiredOption("rendered"));
  validateBackupEvidence(rendered, await readJSON(requiredOption("evidence")));
  print({ ok: true, backup: "verified" });
}

async function retentionManifest() {
  const rendered = await readRendered(requiredOption("rendered"));
  assert(rendered.phase === "teardown", "retention manifest requires teardown phase");
  const stack = await verifyObservedStack(rendered);
  const instanceResponse = await readJSON(requiredOption("instance"));
  const volumeResponse = await readJSON(requiredOption("volume"));
  const securityGroupResponse = await readJSON(requiredOption("security-group"));
  const workloadIAMResponse = await readJSON(requiredOption("workload-iam"));
  const { instance, mapping, volume } = verifyInstance(
    rendered,
    stack,
    instanceResponse,
    volumeResponse,
    securityGroupResponse,
    workloadIAMResponse,
  );
  const snapshotResponse = await readJSON(requiredOption("snapshot"));
  const snapshots = snapshotResponse.Snapshots ?? [];
  assert(snapshots.length === 1, "expected exactly one retained snapshot");
  const snapshot = snapshots[0];
  assert(snapshot.VolumeId === volume.VolumeId, "snapshot volume does not match the root volume");
  assert(snapshot.State === "completed", "retained snapshot must be completed");
  assert(snapshot.Encrypted === true, "retained snapshot must be encrypted");
  assert(
    snapshot.KmsKeyId === rendered.destinations.kmsKeyArn,
    "retained snapshot KMS key drifted",
  );
  const backup = await readJSON(requiredOption("backup-evidence"));
  validateBackupEvidence(rendered, backup);

  const manifest = {
    schema_version: 1,
    environment: "fakeco",
    stack_name: rendered.stackName,
    source_commit: rendered.source.commit,
    runtime_source_commit: backup.source_commit,
    owner_commit: rendered.source.ownerCommit,
    created_at: new Date().toISOString(),
    retained: {
      root_volume: {
        id: volume.VolumeId,
        availability_zone: volume.AvailabilityZone,
        encrypted: true,
        kms_key_arn: volume.KmsKeyId,
        size_gib: volume.Size,
        type: volume.VolumeType,
        delete_on_termination: mapping.Ebs.DeleteOnTermination,
      },
      snapshot: {
        id: snapshot.SnapshotId,
        volume_id: snapshot.VolumeId,
        encrypted: true,
        kms_key_arn: snapshot.KmsKeyId,
        state: snapshot.State,
      },
      sqlite_backup: backup.backup,
      backup_manifest: backup.manifest,
    },
    instance_scheduled_for_termination: {
      id: instance.InstanceId,
      private_ip: instance.PrivateIpAddress,
      vpc_id: instance.VpcId,
      subnet_id: instance.SubnetId,
    },
    deletion_contract: {
      cloudformation_mode: "STANDARD",
      snapshots_deleted: false,
      s3_objects_deleted: false,
      root_volume_delete_on_termination: false,
    },
  };
  await writeJSON(requiredOption("output"), manifest);
  print({ ok: true, retained: ["root-volume", "snapshot", "sqlite-backup"] });
}

async function verifyObservedStack(rendered) {
  return verifyStack(
    rendered,
    await readJSON(requiredOption("stack")),
    await readJSON(requiredOption("template")),
    await readJSON(requiredOption("resources")),
  );
}

function verifyStack(rendered, response, observedTemplate, resourceResponse) {
  const stacks = response.Stacks ?? [];
  assert(stacks.length === 1, "expected exactly one observed stack");
  const stack = stacks[0];
  assert(stack.StackName === rendered.stackName, "observed stack name drifted");
  assert(
    ["CREATE_COMPLETE", "UPDATE_COMPLETE", "UPDATE_ROLLBACK_COMPLETE"].includes(stack.StackStatus),
    "observed stack is not stable",
  );
  assert(
    stack.EnableTerminationProtection === true,
    "stack termination protection must be enabled",
  );
  assert(
    stack.RoleARN === rendered.target.cloudFormationServiceRoleArn,
    "observed stack service role drifted",
  );
  const expectedParameters = new Map(
    rendered.parameters.map((entry) => [entry.ParameterKey, entry.ParameterValue]),
  );
  const observedParameters = new Map(
    (stack.Parameters ?? []).map((entry) => [entry.ParameterKey, entry.ParameterValue]),
  );
  assert(
    expectedParameters.size === observedParameters.size,
    "observed stack parameter set drifted",
  );
  for (const [key, value] of expectedParameters) {
    assert(observedParameters.get(key) === value, `observed stack parameter ${key} drifted`);
  }
  const expectedTags = new Map(rendered.tags.map((entry) => [entry.Key, entry.Value]));
  const observedTags = new Map(
    customerManagedTags(stack.Tags ?? []).map((entry) => [entry.Key, entry.Value]),
  );
  assert(expectedTags.size === observedTags.size, "observed stack tag set drifted");
  for (const [key, value] of expectedTags) {
    assert(observedTags.get(key) === value, `observed stack tag ${key} drifted`);
  }
  const outputs = new Map(
    (stack.Outputs ?? []).map((entry) => [entry.OutputKey, entry.OutputValue]),
  );
  assert(outputs.get("SourceCommit") === rendered.source.commit, "observed source commit drifted");
  assert(outputs.get("VpcId") === rendered.target.vpcId, "observed VPC drifted");
  assert(outputs.get("PrivateSubnetId") === rendered.target.subnetId, "observed subnet drifted");
  for (const key of [
    "InstanceId",
    "PrivateIp",
    "SecurityGroupId",
    "InstanceProfileArn",
    "InstanceProfileName",
    "InstanceRoleArn",
    "InstanceRoleName",
  ]) {
    assert(
      typeof outputs.get(key) === "string" && outputs.get(key).length > 0,
      `stack output ${key} is missing`,
    );
  }
  verifyStackInventory(rendered, observedTemplate, resourceResponse);
  return stack;
}

function verifyStackInventory(rendered, observedTemplate, response) {
  assert(
    canonicalJSON(observedTemplate) === canonicalJSON(template),
    "observed stack template drifted",
  );
  const expectedResources = Object.entries(template.Resources).filter(([, resource]) => {
    if (resource.Condition === undefined) return true;
    assert(resource.Condition === "AllowMetricsSource", "template resource condition drifted");
    return rendered.target.metricsSecurityGroupId !== "";
  });
  const observedResources = response.StackResources ?? [];
  assert(
    observedResources.length === expectedResources.length,
    "observed stack resource set drifted",
  );
  const observedByLogicalID = new Map(
    observedResources.map((resource) => [resource.LogicalResourceId, resource]),
  );
  assert(
    observedByLogicalID.size === observedResources.length,
    "observed stack resource IDs are not unique",
  );
  for (const [logicalID, expected] of expectedResources) {
    const observed = observedByLogicalID.get(logicalID);
    assert(observed?.StackName === rendered.stackName, `stack resource ${logicalID} owner drifted`);
    assert(observed?.ResourceType === expected.Type, `stack resource ${logicalID} type drifted`);
    assert(
      typeof observed?.PhysicalResourceId === "string" && observed.PhysicalResourceId.length > 0,
      `stack resource ${logicalID} physical ID is missing`,
    );
    assert(
      typeof observed?.ResourceStatus === "string" &&
        observed.ResourceStatus.endsWith("_COMPLETE") &&
        !observed.ResourceStatus.startsWith("DELETE"),
      `stack resource ${logicalID} is not stable`,
    );
  }
}

function verifyInstance(
  rendered,
  stack,
  response,
  volumeResponse,
  securityGroupResponse,
  workloadIAMResponse,
) {
  const reservations = response.Reservations ?? [];
  const instances = reservations.flatMap((reservation) => reservation.Instances ?? []);
  assert(instances.length === 1, "expected exactly one observed instance");
  const instance = instances[0];
  const outputs = new Map(stack.Outputs.map((entry) => [entry.OutputKey, entry.OutputValue]));
  assert(instance.InstanceId === outputs.get("InstanceId"), "observed instance ID drifted");
  assert(instance.InstanceType === profile.instance.type, "observed instance type drifted");
  assert(
    instance.Architecture === profile.instance.architecture,
    "observed instance architecture drifted",
  );
  assert(instance.ImageId === rendered.target.imageId, "observed AMI drifted");
  assert(instance.VpcId === rendered.target.vpcId, "observed instance VPC drifted");
  assert(instance.SubnetId === rendered.target.subnetId, "observed instance subnet drifted");
  assert(instance.PrivateIpAddress === outputs.get("PrivateIp"), "observed private IP drifted");
  assert(instance.PublicIpAddress === undefined, "instance must not have a public IP");
  assert(instance.KeyName === undefined, "instance must not have an SSH key");
  assert(instance.State?.Name === "running", "instance must be running");
  assert(instance.MetadataOptions?.HttpTokens === "required", "observed IMDS token mode drifted");
  assert(
    instance.MetadataOptions?.HttpPutResponseHopLimit === 1,
    "observed IMDS hop limit drifted",
  );
  assert(instance.SecurityGroups?.length === 1, "instance must have exactly one security group");
  assert(
    instance.SecurityGroups[0].GroupId === outputs.get("SecurityGroupId"),
    "observed instance security group drifted",
  );
  assert(
    instance.IamInstanceProfile?.Arn === outputs.get("InstanceProfileArn"),
    "observed instance profile drifted",
  );
  const mappings = instance.BlockDeviceMappings ?? [];
  assert(mappings.length === 1, "instance must have exactly one block device mapping");
  const mapping = mappings.find((entry) => entry.DeviceName === profile.instance.rootDeviceName);
  assert(mapping?.Ebs?.VolumeId, "observed root volume mapping is missing");
  assert(
    mapping.Ebs.DeleteOnTermination === false,
    "observed root volume must survive termination",
  );
  const volumes = volumeResponse.Volumes ?? [];
  assert(volumes.length === 1, "expected exactly one observed root volume");
  const volume = volumes[0];
  assert(volume.VolumeId === mapping.Ebs.VolumeId, "observed root volume ID drifted");
  assert(volume.Size === profile.instance.rootVolumeGiB, "observed root volume size drifted");
  assert(
    volume.VolumeType === profile.instance.rootVolumeType,
    "observed root volume type drifted",
  );
  assert(volume.Encrypted === true, "observed root volume must be encrypted");
  assert(
    volume.KmsKeyId === rendered.destinations.kmsKeyArn,
    "observed root volume KMS key drifted",
  );
  assert(volume.State === "in-use", "observed root volume must be attached");
  verifySecurityGroup(rendered, stack, securityGroupResponse);
  verifyWorkloadIAM(rendered, stack, workloadIAMResponse);
  return { instance, mapping, volume };
}

function verifySecurityGroup(rendered, stack, response) {
  const groups = response.SecurityGroups ?? [];
  assert(groups.length === 1, "expected exactly one observed ClickClack security group");
  const group = groups[0];
  const outputs = new Map(stack.Outputs.map((entry) => [entry.OutputKey, entry.OutputValue]));
  assert(group.GroupId === outputs.get("SecurityGroupId"), "observed security group ID drifted");
  assert(group.VpcId === rendered.target.vpcId, "observed security group VPC drifted");

  const expectedSources = new Set([rendered.target.gatewaySecurityGroupId]);
  if (rendered.target.metricsSecurityGroupId !== "") {
    expectedSources.add(rendered.target.metricsSecurityGroupId);
  }
  const observedSources = [];
  for (const permission of group.IpPermissions ?? []) {
    assert(permission.IpProtocol === "tcp", "observed ingress protocol drifted");
    assert(
      permission.FromPort === 8080 && permission.ToPort === 8080,
      "observed ingress port drifted",
    );
    assert((permission.IpRanges ?? []).length === 0, "observed IPv4 CIDR ingress is forbidden");
    assert((permission.Ipv6Ranges ?? []).length === 0, "observed IPv6 CIDR ingress is forbidden");
    assert(
      (permission.PrefixListIds ?? []).length === 0,
      "observed prefix-list ingress is forbidden",
    );
    const sourceGroups = permission.UserIdGroupPairs ?? [];
    assert(sourceGroups.length > 0, "observed ingress lacks a source security group");
    for (const source of sourceGroups) {
      assert(
        source.UserId === rendered.target.accountId,
        "observed ingress source account drifted",
      );
      assert(
        source.VpcPeeringConnectionId === undefined,
        "observed peered security-group ingress is forbidden",
      );
      observedSources.push(source.GroupId);
    }
  }
  assert(observedSources.length === expectedSources.size, "observed ingress source count drifted");
  assert(
    new Set(observedSources).size === observedSources.length,
    "observed ingress contains duplicate sources",
  );
  for (const source of observedSources) {
    assert(expectedSources.has(source), "observed ingress source security group drifted");
  }
}

function verifyWorkloadIAM(rendered, stack, response) {
  const outputs = new Map(stack.Outputs.map((entry) => [entry.OutputKey, entry.OutputValue]));
  const instanceProfile = response.InstanceProfile;
  assert(instanceProfile, "observed instance profile is missing");
  assert(
    instanceProfile.InstanceProfileName === outputs.get("InstanceProfileName"),
    "observed instance profile name drifted",
  );
  assert(instanceProfile.Arn === outputs.get("InstanceProfileArn"), "observed profile ARN drifted");
  assert(
    instanceProfile.Path === "/openclaw/fakeco/clickclack/",
    "observed instance profile path drifted",
  );
  assert(instanceProfile.Roles?.length === 1, "instance profile must contain exactly one role");
  assert(
    instanceProfile.Roles[0].RoleName === outputs.get("InstanceRoleName") &&
      instanceProfile.Roles[0].Arn === outputs.get("InstanceRoleArn"),
    "observed instance profile role drifted",
  );

  const role = response.Role;
  assert(role, "observed workload role is missing");
  assert(role.RoleName === outputs.get("InstanceRoleName"), "observed workload role name drifted");
  assert(role.Arn === outputs.get("InstanceRoleArn"), "observed workload role ARN drifted");
  assert(role.Path === "/openclaw/fakeco/clickclack/", "observed workload role path drifted");
  assert(
    role.PermissionsBoundary?.PermissionsBoundaryArn === rendered.target.permissionsBoundaryArn &&
      role.PermissionsBoundary?.PermissionsBoundaryType === "Policy",
    "observed workload permissions boundary drifted",
  );
  assert(
    canonicalJSON(role.AssumeRolePolicyDocument) ===
      canonicalJSON(template.Resources.InstanceRole.Properties.AssumeRolePolicyDocument),
    "observed workload trust policy drifted",
  );
  verifyExactTags(rendered.tags, customerManagedTags(role.Tags ?? []), "workload role");
  assert(
    (response.AttachedPolicies ?? []).length === 0,
    "managed policies are forbidden on the workload role",
  );

  const expectedPolicies = new Map(
    template.Resources.InstanceRole.Properties.Policies.map((entry) => [
      entry.PolicyName,
      resolveTemplateParameters(entry.PolicyDocument, rendered),
    ]),
  );
  const observedNames = response.PolicyNames ?? [];
  assert(observedNames.length === expectedPolicies.size, "observed inline policy name set drifted");
  assert(
    new Set(observedNames).size === observedNames.length,
    "observed inline policy names contain duplicates",
  );
  for (const name of observedNames) {
    assert(expectedPolicies.has(name), "observed inline policy name drifted");
  }
  const observedPolicies = response.RolePolicies ?? [];
  assert(observedPolicies.length === expectedPolicies.size, "observed inline policy set drifted");
  for (const observed of observedPolicies) {
    const expected = expectedPolicies.get(observed.PolicyName);
    assert(expected, "observed inline policy document has an unexpected name");
    assert(
      canonicalJSON(observed.PolicyDocument) === canonicalJSON(expected),
      `observed inline policy ${observed.PolicyName} drifted`,
    );
  }
}

function resolveTemplateParameters(value, rendered) {
  if (Array.isArray(value)) {
    return value.map((entry) => resolveTemplateParameters(entry, rendered));
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  const parameters = new Map(
    rendered.parameters.map((entry) => [entry.ParameterKey, entry.ParameterValue]),
  );
  if (Object.keys(value).length === 1 && typeof value.Ref === "string") {
    assert(parameters.has(value.Ref), `unknown policy parameter ${value.Ref}`);
    return parameters.get(value.Ref);
  }
  if (Object.keys(value).length === 1 && typeof value["Fn::Sub"] === "string") {
    return value["Fn::Sub"].replace(/\$\{([^}]+)\}/gu, (_match, name) => {
      assert(parameters.has(name), `unknown policy substitution ${name}`);
      return parameters.get(name);
    });
  }
  return Object.fromEntries(
    Object.entries(value).map(([key, entry]) => [key, resolveTemplateParameters(entry, rendered)]),
  );
}

function verifyExactTags(expectedTags, observedTags, label) {
  const expected = new Map(expectedTags.map((entry) => [entry.Key, entry.Value]));
  const observed = new Map(observedTags.map((entry) => [entry.Key, entry.Value]));
  assert(expected.size === observed.size, `observed ${label} tag set drifted`);
  for (const [key, value] of expected) {
    assert(observed.get(key) === value, `observed ${label} tag ${key} drifted`);
  }
}

function customerManagedTags(tags) {
  return tags.filter((entry) => !entry.Key.toLowerCase().startsWith("aws:"));
}

function canonicalJSON(value) {
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJSON).sort().join(",")}]`;
  }
  if (value !== null && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

function validateBackupEvidence(rendered, evidence) {
  assert(evidence.schema_version === 1, "backup evidence schema drifted");
  assert(evidence.status === "passed", "backup evidence did not pass");
  const expectedAction = { apply: "bootstrap", verify: "verify", teardown: "backup" }[
    rendered.phase
  ];
  assert(expectedAction !== undefined, "rendered phase does not produce backup evidence");
  assert(evidence.action === expectedAction, "backup action drifted");
  assert(
    evidence.stack_source_commit === rendered.source.commit,
    "backup stack source commit drifted",
  );
  validateCommit(evidence.source_commit, "backup runtime source commit");
  if (rendered.phase !== "teardown") {
    assert(evidence.source_commit === rendered.source.commit, "backup source commit drifted");
  }
  assert(evidence.owner_commit === rendered.source.ownerCommit, "backup owner commit drifted");
  assert(evidence.runtime_commit_verified === true, "backup evidence lacks runtime commit proof");
  assert(/^sha256:[0-9a-f]{64}$/u.test(evidence.image_id ?? ""), "backup image ID is invalid");
  assert(evidence.seed_equal === true, "backup evidence lacks seed rerun equality");
  assert(
    /^[0-9a-f]{64}$/u.test(evidence.seed_manifest_sha256 ?? ""),
    "backup evidence lacks a valid seed manifest SHA-256",
  );
  assert(
    evidence.health === true && evidence.readiness === true,
    "backup evidence lacks health/readiness proof",
  );
  assert(
    evidence.metrics_metadata_only === true,
    "backup evidence lacks metadata-only metrics proof",
  );
  assert(evidence.integrity_check === "ok", "backup SQLite integrity proof failed");
  assert(evidence.backup?.bucket === rendered.destinations.backups.bucket, "backup bucket drifted");
  assert(
    evidence.backup?.key?.startsWith(
      `${rendered.destinations.backups.prefix}/sqlite/${evidence.source_commit}/`,
    ),
    "backup object escaped the locked runtime prefix",
  );
  assert(/^[0-9a-f]{64}$/u.test(evidence.backup?.sha256 ?? ""), "backup SHA-256 is invalid");
  assert(
    evidence.manifest?.bucket === rendered.destinations.backups.bucket,
    "backup manifest bucket drifted",
  );
  assert(
    evidence.manifest?.key?.startsWith(`${rendered.destinations.backups.prefix}/manifests/`),
    "backup manifest escaped the locked prefix",
  );
}

async function readRendered(file) {
  const rendered = await readJSON(file);
  assert(rendered.schemaVersion === 1, "rendered owner schema drifted");
  assert(rendered.stackName === profile.stackName, "rendered stack name drifted");
  validateCommit(rendered.source?.commit, "rendered source commit");
  validateCommit(rendered.source?.ownerCommit, "rendered owner commit");
  return rendered;
}

function destination(kind) {
  const bucket = requiredEnv(`FAKECO_${kind}_BUCKET`);
  assert(
    /^(?![0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$)[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/u.test(bucket),
    `FAKECO_${kind}_BUCKET is not a valid bucket name`,
  );
  const prefix = requiredEnv(`FAKECO_${kind}_PREFIX`);
  assert(
    /^clickclack\/fakeco\/[a-z0-9][a-z0-9/_-]*[a-z0-9]$/u.test(prefix) &&
      !prefix.includes("//") &&
      !prefix.endsWith("/"),
    `FAKECO_${kind}_PREFIX must be a normalized clickclack/fakeco prefix`,
  );
  return { bucket, prefix, arn: `arn:aws:s3:::${bucket}` };
}

function assertDestinationsDoNotOverlap(destinations) {
  for (let left = 0; left < destinations.length; left += 1) {
    for (let right = left + 1; right < destinations.length; right += 1) {
      const [leftLabel, leftDestination] = destinations[left];
      const [rightLabel, rightDestination] = destinations[right];
      if (leftDestination.bucket !== rightDestination.bucket) continue;
      const overlap =
        leftDestination.prefix === rightDestination.prefix ||
        leftDestination.prefix.startsWith(`${rightDestination.prefix}/`) ||
        rightDestination.prefix.startsWith(`${leftDestination.prefix}/`);
      assert(
        !overlap,
        `${leftLabel} and ${rightLabel} prefixes must not overlap when buckets are shared`,
      );
    }
  }
}

function scalarList(value) {
  if (value === undefined) return [];
  return Array.isArray(value) ? value : [value];
}

function actionPatternAllows(pattern, action) {
  if (typeof pattern !== "string") return false;
  const normalizedPattern = pattern.toLowerCase();
  const normalizedAction = action.toLowerCase();
  if (!normalizedPattern.endsWith("*")) return normalizedPattern === normalizedAction;
  return normalizedAction.startsWith(normalizedPattern.slice(0, -1));
}

function exactRoleArn(value, accountId, rolePath, label) {
  const expected = `arn:aws:iam::${accountId}:role/${rolePath}`;
  assert(value === expected, `${label} ARN does not match the locked FakeCo path`);
  return value;
}

function parameter(ParameterKey, ParameterValue) {
  return { ParameterKey, ParameterValue };
}

function validateCommit(value, label) {
  assert(
    typeof value === "string" && /^[0-9a-f]{40}$/u.test(value),
    `${label} must be a full lowercase commit SHA`,
  );
}

function validateId(value, pattern, label) {
  assert(pattern.test(value), `${label} is invalid`);
  return value;
}

function requiredEnv(name) {
  const value = optionalEnv(name);
  assert(value !== "", `${name} is required`);
  return value;
}

function optionalEnv(name) {
  return (process.env[name] ?? "").trim();
}

function requiredOption(name) {
  const value = options[name];
  assert(typeof value === "string" && value !== "", `--${name} is required`);
  return value;
}

function parseOptions(values) {
  const parsed = {};
  for (let index = 0; index < values.length; index += 2) {
    const name = values[index];
    const value = values[index + 1];
    if (!name?.startsWith("--") || value === undefined || value.startsWith("--")) {
      throw new Error(`invalid option near ${name ?? "end of arguments"}`);
    }
    const key = name.slice(2);
    if (parsed[key] !== undefined) {
      throw new Error(`duplicate option --${key}`);
    }
    parsed[key] = value;
  }
  return parsed;
}

async function readJSON(file) {
  return JSON.parse(await readFile(file, "utf8"));
}

async function writeJSON(file, value) {
  await writeFile(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
}

function print(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}
