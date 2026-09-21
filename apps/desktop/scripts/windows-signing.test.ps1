param(
    [string]$UnsignedReleaseDirectory,
    [string]$Version,
    [string]$VendorElevate
)

$proof = @{ Directory = $UnsignedReleaseDirectory; Version = $Version; Reference = $VendorElevate }
. "$PSScriptRoot/verify-windows-release.ps1"

function Assert-Throws([scriptblock]$Action, [string]$Message) {
    $failed = $false
    try { & $Action } catch {
        if ($_.Exception.Message -notmatch $Message) { throw }
        $failed = $true
    }
    if (-not $failed) { throw "Expected failure matching: $Message" }
}

$fixture = Join-Path ([IO.Path]::GetTempPath()) ("clickclack-signing-test-" + [guid]::NewGuid())
New-Item -Path $fixture -ItemType Directory | Out-Null
try {
    $owned = Join-Path $fixture 'ClickClack.exe'
    $vendor = Join-Path $fixture 'elevate.exe'
    $reference = Join-Path $fixture 'reference.exe'
    [IO.File]::WriteAllText($owned, 'owned fixture')
    [IO.File]::WriteAllText($vendor, 'vendor fixture')
    Copy-Item -LiteralPath $vendor -Destination $reference

    # Signature metadata fixtures exercise policy; they do not claim cryptographic proof.
    $valid = @{
        Status = 'Valid'
        SignatureType = 'Authenticode'
        SignerCertificate = [pscustomobject]@{ Subject = $script:FoundationPublisher }
        TimeStamperCertificate = [pscustomobject]@{ Subject = 'Timestamp authority fixture' }
    }
    $unsigned = @{
        Status = 'NotSigned'
        SignatureType = 'None'
        SignerCertificate = $null
        TimeStamperCertificate = $null
    }
    $script:fixtureSignature = [pscustomobject]$valid
    $script:vendorSignatures = @{}
    function Get-AuthenticodeSignature([string]$LiteralPath) {
        if ($script:vendorSignatures.ContainsKey($LiteralPath)) {
            return $script:vendorSignatures[$LiteralPath]
        }
        return $script:fixtureSignature
    }

    Assert-OwnedSignature $owned
    foreach ($status in 'NotSigned', 'HashMismatch', 'NotTrusted', 'UnknownError') {
        $changed = $valid.Clone()
        $changed.Status = $status
        $script:fixtureSignature = [pscustomobject]$changed
        Assert-Throws { Assert-OwnedSignature $owned } 'valid, timestamped'
    }
    foreach ($field in 'SignerCertificate', 'TimeStamperCertificate') {
        $changed = $valid.Clone()
        $changed[$field] = $null
        $script:fixtureSignature = [pscustomobject]$changed
        Assert-Throws { Assert-OwnedSignature $owned } 'valid, timestamped'
    }
    $changed = $valid.Clone()
    $changed.SignerCertificate = [pscustomobject]@{ Subject = 'CN=Another publisher' }
    $script:fixtureSignature = [pscustomobject]$changed
    Assert-Throws { Assert-OwnedSignature $owned } 'valid, timestamped'
    $changed = $valid.Clone()
    $changed.SignatureType = 'Catalog'
    $script:fixtureSignature = [pscustomobject]$changed
    Assert-Throws { Assert-OwnedSignature $owned } 'valid, timestamped'
    $script:fixtureSignature = [pscustomobject]$valid
    Assert-Throws { Assert-OwnedSignature (Join-Path $fixture 'missing.exe') } 'Missing owned'

    $script:vendorSignatures[$vendor] = [pscustomobject]$unsigned
    Assert-VendorBinary $vendor $reference
    [IO.File]::WriteAllText($vendor, 'changed vendor fixture')
    Assert-Throws { Assert-VendorBinary $vendor $reference } 'differs from the pinned NSIS'
    Copy-Item -LiteralPath $reference -Destination $vendor -Force
    $script:vendorSignatures[$vendor] = [pscustomobject]$valid
    Assert-Throws { Assert-VendorBinary $vendor $reference } 'retain its vendor ownership'
    Assert-Throws { Assert-VendorBinary $vendor (Join-Path $fixture 'missing-reference.exe') } 'Missing vendor'

    $appDirectory = Join-Path $fixture 'app'
    New-Item -Path (Join-Path $appDirectory 'resources') -ItemType Directory -Force | Out-Null
    Copy-Item -LiteralPath $owned -Destination (Join-Path $appDirectory 'ClickClack.exe')
    [IO.File]::WriteAllText((Join-Path $appDirectory 'resources/app.asar'), 'application fixture')
    $appHashes = Get-PayloadHashes $appDirectory
    Assert-SamePayload $appHashes $appHashes.Clone()
    Assert-Throws { Assert-SamePayload $appHashes @{} } 'inventories differ'
    $changedHashes = $appHashes.Clone()
    $changedHashes['ClickClack.exe'] = 'changed'
    Assert-Throws { Assert-SamePayload $appHashes $changedHashes } 'payloads differ'
    $changedHashes = $appHashes.Clone()
    $changedHashes.Remove('resources/app.asar')
    $changedHashes['different.asar'] = 'changed'
    Assert-Throws { Assert-SamePayload $appHashes $changedHashes } 'payloads differ'

    $extra = Join-Path $appDirectory 'unexpected.exe'
    Copy-Item -LiteralPath $owned -Destination $extra
    Assert-Throws { Get-PayloadHashes $appDirectory } 'Unexpected executable'
    Remove-Item -LiteralPath $extra
    $helper = Join-Path $appDirectory 'resources/elevate.exe'
    Copy-Item -LiteralPath $reference -Destination $helper
    Assert-Throws { Get-PayloadHashes $appDirectory } 'Unexpected executable'
    Assert-SamePayload (Get-PayloadHashes $appDirectory -Nsis) $appHashes
    $dll = Join-Path $appDirectory 'vendor.dll'
    [IO.File]::WriteAllText($dll, 'vendor library fixture')
    Assert-Throws { Get-PayloadHashes $appDirectory -Nsis } 'retain its vendor ownership'
    $script:vendorSignatures[$dll] = [pscustomobject]$unsigned
    $null = Get-PayloadHashes $appDirectory -Nsis

    # The process receives no Azure identity. Only fake selector values are used.
    foreach ($name in 'AZURE_CLIENT_ID', 'AZURE_TENANT_ID', 'AZURE_SUBSCRIPTION_ID') {
        [Environment]::SetEnvironmentVariable($name, 'fixture')
    }
    Assert-WindowsSigningInputs
    foreach ($name in 'AZURE_CLIENT_ID', 'AZURE_TENANT_ID', 'AZURE_SUBSCRIPTION_ID') {
        [Environment]::SetEnvironmentVariable($name, ' ')
        Assert-Throws { Assert-WindowsSigningInputs } "Missing $name"
        [Environment]::SetEnvironmentVariable($name, 'fixture')
    }
    Write-Host 'Windows signature, publisher, timestamp, inventory, vendor, and missing-input policy fixtures passed.'
} finally {
    Remove-Item Function:Get-AuthenticodeSignature -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $fixture -Recurse -Force
    foreach ($name in 'AZURE_CLIENT_ID', 'AZURE_TENANT_ID', 'AZURE_SUBSCRIPTION_ID') {
        [Environment]::SetEnvironmentVariable($name, $null)
    }
}

if (-not [string]::IsNullOrWhiteSpace($proof.Directory)) {
    # First exercise the unchanged production gate against the real unsigned installer.
    Assert-Throws {
        Assert-WindowsRelease $proof.Version $proof.Directory $proof.Reference
    } 'valid, timestamped'

    # Test-only substitution: require unsigned ownership while exercising real extraction
    # and byte comparisons. The production entrypoint has no unsigned/skip-signature mode.
    $script:unsignedPaths = [Collections.Generic.List[string]]::new()
    function Assert-OwnedSignature([string]$Path) {
        if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Missing preview executable: $Path" }
        $signature = Get-AuthenticodeSignature -LiteralPath $Path
        if ($signature.Status -ne 'NotSigned' -or $null -ne $signature.SignerCertificate) {
            throw "Expected an unsigned preview executable: $Path"
        }
        $script:unsignedPaths.Add($Path)
    }
    Assert-WindowsRelease $proof.Version $proof.Directory $proof.Reference
    if ($script:unsignedPaths.Count -ne 4) {
        throw "Expected installer, uninstaller, NSIS app and ZIP app checks; got $($script:unsignedPaths.Count)."
    }
    Write-Host 'Unsigned preview rejected by production gate; real NSIS/ZIP extraction, vendor bytes and payload equality passed. No positive signature proof.'
}
