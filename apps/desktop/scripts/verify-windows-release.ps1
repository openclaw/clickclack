param(
    [string]$Version,
    [string]$ReleaseDirectory,
    [string]$VendorElevate
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$script:FoundationPublisher = 'CN=OpenClaw Foundation, O=OpenClaw Foundation, L=Mill Valley, S=California, C=US'

function Assert-WindowsSigningInputs {
    foreach ($name in 'AZURE_CLIENT_ID', 'AZURE_TENANT_ID', 'AZURE_SUBSCRIPTION_ID') {
        if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
            throw "Missing $name. Configure the approved Windows identity in the release-signing environment before releasing."
        }
    }
}

function Assert-OwnedSignature([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Missing owned executable: $Path"
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne 'Valid' -or
        $signature.SignatureType -ne 'Authenticode' -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.Subject -cne $script:FoundationPublisher -or
        $null -eq $signature.TimeStamperCertificate) {
        throw "Expected a valid, timestamped OpenClaw Foundation Authenticode signature: $Path"
    }
}

function Assert-VendorBinary([string]$Path, [string]$Reference) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf) -or
        -not (Test-Path -LiteralPath $Reference -PathType Leaf)) {
        throw "Missing vendor binary or pinned toolset reference: $Path"
    }
    if ((Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash -cne
        (Get-FileHash -LiteralPath $Reference -Algorithm SHA256).Hash) {
        throw "Vendor binary differs from the pinned NSIS toolset: $Path"
    }
    Assert-NotFoundationSigned $Path
}

function Assert-NotFoundationSigned([string]$Path) {
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($null -ne $signature.SignerCertificate -and
        $signature.SignerCertificate.Subject -ceq $script:FoundationPublisher) {
        throw "Third-party binary must retain its vendor ownership: $Path"
    }
}

function Get-PayloadHashes([string]$Directory, [switch]$Nsis) {
    $app = Join-Path $Directory 'ClickClack.exe'
    Assert-OwnedSignature $app
    $hashes = @{}
    foreach ($file in Get-ChildItem -LiteralPath $Directory -Recurse -File) {
        $relative = [IO.Path]::GetRelativePath($Directory, $file.FullName).Replace('\', '/')
        if ($Nsis -and $relative -ceq 'resources/elevate.exe') {
            continue
        }
        if ($file.Extension -ieq '.exe' -and $relative -cne 'ClickClack.exe') {
            throw "Unexpected executable in desktop payload: $relative"
        }
        if ($file.Extension -in '.dll', '.node') {
            Assert-NotFoundationSigned $file.FullName
        }
        $hashes[$relative] = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash
    }
    return $hashes
}

function Assert-SamePayload([hashtable]$Installer, [hashtable]$Zip) {
    if ($Installer.Count -ne $Zip.Count) {
        throw 'NSIS and ZIP payload inventories differ.'
    }
    foreach ($name in $Installer.Keys) {
        if (-not $Zip.ContainsKey($name) -or $Installer[$name] -cne $Zip[$name]) {
            throw "NSIS and ZIP payloads differ: $name"
        }
    }
}

function Expand-ReleaseArchive([string]$Path, [string]$Destination) {
    & 7z x -y "-o$Destination" -- $Path | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Could not extract release archive: $Path"
    }
}

function Assert-WindowsRelease([string]$ReleaseVersion, [string]$Directory, [string]$ElevateReference) {
    if ($ReleaseVersion -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$') {
        throw 'A semantic release version is required.'
    }
    $Directory = (Resolve-Path -LiteralPath $Directory).Path
    $installer = Join-Path $Directory "ClickClack-$ReleaseVersion-win-x64.exe"
    $zip = Join-Path $Directory "ClickClack-$ReleaseVersion-win-x64.zip"
    Assert-OwnedSignature $installer
    if (-not (Test-Path -LiteralPath $zip -PathType Leaf)) {
        throw "Missing Windows ZIP: $zip"
    }
    $scratch = Join-Path ([IO.Path]::GetTempPath()) ("clickclack-windows-verify-" + [guid]::NewGuid())
    try {
        $outer = Join-Path $scratch 'installer'
        $nsisApp = Join-Path $scratch 'nsis-app'
        $zipApp = Join-Path $scratch 'zip-app'
        Expand-ReleaseArchive $installer $outer
        $uninstallers = @(Get-ChildItem -LiteralPath $outer -Recurse -File -Filter '*.exe')
        if ($uninstallers.Count -ne 1 -or $uninstallers[0].Name -cne 'Uninstall ClickClack.exe') {
            throw 'Expected exactly the ClickClack uninstaller in the NSIS envelope.'
        }
        Assert-OwnedSignature $uninstallers[0].FullName
        $archives = @(Get-ChildItem -LiteralPath $outer -Recurse -File -Filter 'app-*.7z')
        if ($archives.Count -ne 1 -or $archives[0].Name -cne 'app-64.7z') {
            throw 'Expected exactly one x64 NSIS app payload.'
        }
        Expand-ReleaseArchive $archives[0].FullName $nsisApp
        Expand-ReleaseArchive $zip $zipApp
        # Builder 26.16.1 injects elevate only into NSIS, never the concurrent ZIP.
        Assert-VendorBinary (Join-Path $nsisApp 'resources/elevate.exe') $ElevateReference
        $installerHashes = Get-PayloadHashes $nsisApp -Nsis
        $zipHashes = Get-PayloadHashes $zipApp
        Assert-SamePayload $installerHashes $zipHashes
    } finally {
        if (Test-Path -LiteralPath $scratch) {
            Remove-Item -LiteralPath $scratch -Recurse -Force
        }
    }
}

if ($MyInvocation.InvocationName -ne '.') {
    if ([string]::IsNullOrWhiteSpace($Version) -or
        [string]::IsNullOrWhiteSpace($ReleaseDirectory) -or
        [string]::IsNullOrWhiteSpace($VendorElevate)) {
        throw 'Usage: verify-windows-release.ps1 -Version <version> -ReleaseDirectory <directory> -VendorElevate <pinned-toolset-elevate.exe>'
    }
    Assert-WindowsRelease $Version $ReleaseDirectory $VendorElevate
    Write-Host "Verified Foundation signatures, timestamps, vendor helper, and identical Windows payloads for $Version."
}
