param(
  [Parameter(Mandatory = $true)][string]$InstallDir,
  [Parameter(Mandatory = $true)][string]$AppVersion,
  [Parameter(Mandatory = $true)][string]$AppBaseUrl,
  [Parameter(Mandatory = $true)][string]$RuntimeVersion,
  [Parameter(Mandatory = $true)][string]$RuntimeBaseUrl
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Add-Type -AssemblyName System.IO.Compression.FileSystem

function Normalize-VersionTag([string]$Version) {
  if ($Version.StartsWith("v") -or $Version.StartsWith("runtime-v")) { return $Version }
  return "v$Version"
}

$appTag = Normalize-VersionTag $AppVersion
$runtimeTag = Normalize-VersionTag $RuntimeVersion
$cache = Join-Path $env:TEMP "HindsightLocalManagerInstall-$appTag-$runtimeTag"
$runtimeInstallRoot = Join-Path ([Environment]::GetFolderPath("LocalApplicationData")) "HLM\r"
$runtimeResourcesRoot = Join-Path $runtimeInstallRoot "resources"
New-Item -ItemType Directory -Path $InstallDir, $cache, $runtimeInstallRoot -Force | Out-Null

$components = @(
  @{ Name = "App"; Version = $appTag; BaseUrl = $AppBaseUrl; Asset = "Hindsight-Local-Manager-$appTag-app.zip"; Marker = ".app-version"; Destination = $InstallDir; MarkerRoot = $InstallDir },
  @{ Name = "Python runtime"; Version = $runtimeTag; BaseUrl = $RuntimeBaseUrl; Asset = "Hindsight-Local-Manager-$runtimeTag-python.zip"; Marker = ".python-version"; Destination = $runtimeInstallRoot; MarkerRoot = $runtimeInstallRoot },
  @{ Name = "Node runtime"; Version = $runtimeTag; BaseUrl = $RuntimeBaseUrl; Asset = "Hindsight-Local-Manager-$runtimeTag-node.zip"; Marker = ".node-version"; Destination = $runtimeInstallRoot; MarkerRoot = $runtimeInstallRoot },
  @{ Name = "Hindsight UI"; Version = $runtimeTag; BaseUrl = $RuntimeBaseUrl; Asset = "Hindsight-Local-Manager-$runtimeTag-control-plane.zip"; Marker = ".control-plane-version"; Destination = $runtimeInstallRoot; MarkerRoot = $runtimeInstallRoot }
)

function Download-FileWithProgress {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [Parameter(Mandatory = $true)][string]$OutFile,
    [Parameter(Mandatory = $true)][string]$Label
  )

  Write-Output "[$Label] Starting download"
  $request = [Net.HttpWebRequest]::Create($Url)
  $request.UserAgent = "HindsightLocalManagerInstaller/$appTag"
  $response = $request.GetResponse()
  try {
    $total = [int64]$response.ContentLength
    if ($total -gt 0 -and (Test-Path -LiteralPath $OutFile) -and ((Get-Item -LiteralPath $OutFile).Length -eq $total)) {
      Write-Output "[$Label] Using cached download ($([math]::Round($total / 1MB, 1)) MB)"
      return
    }
    $inputStream = $response.GetResponseStream()
    $outputStream = [IO.File]::Open($OutFile, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
      $buffer = New-Object byte[] (1024 * 1024)
      $readTotal = [int64]0
      $lastPercent = -1
      while (($read = $inputStream.Read($buffer, 0, $buffer.Length)) -gt 0) {
        $outputStream.Write($buffer, 0, $read)
        $readTotal += $read
        if ($total -gt 0) {
          $percent = [int][math]::Floor(($readTotal * 100) / $total)
          if ($percent -ge $lastPercent + 5 -or $percent -eq 100) {
            $lastPercent = $percent
            Write-Output "[$Label] Download $percent% ($([math]::Round($readTotal / 1MB, 1)) MB / $([math]::Round($total / 1MB, 1)) MB)"
          }
        } else {
          Write-Output "[$Label] Downloaded $([math]::Round($readTotal / 1MB, 1)) MB"
        }
      }
    } finally {
      $outputStream.Dispose()
      $inputStream.Dispose()
    }
  } finally {
    $response.Dispose()
  }
}

function Expand-ZipWithProgress {
  param(
    [Parameter(Mandatory = $true)][string]$Zip,
    [Parameter(Mandatory = $true)][string]$Destination,
    [Parameter(Mandatory = $true)][string]$Label
  )

  $destinationRoot = [IO.Path]::GetFullPath($Destination)

  $archive = [IO.Compression.ZipFile]::OpenRead($Zip)
  try {
    $entries = @($archive.Entries | Where-Object { $_.FullName -and !$_.FullName.EndsWith("/") })
    $total = $entries.Count
    $done = 0
    $lastPercent = -1

    Write-Output "[$Label] Extracting 0% (0 / $total files)"
    foreach ($entry in $entries) {
      $relativePath = $entry.FullName -replace '/', [IO.Path]::DirectorySeparatorChar
      $parts = @($relativePath -split '[\\/]')
      if ([IO.Path]::IsPathRooted($relativePath) -or ($parts | Where-Object { $_ -eq '..' })) {
        throw "Unsafe zip entry path: $($entry.FullName)"
      }

      $target = Join-Path $destinationRoot $relativePath
      $targetDir = [IO.Path]::GetDirectoryName($target)
      if ($target.Length -ge 260 -or $targetDir.Length -ge 248) {
        Write-Output "[$Label] Skipping overlong metadata path: $($entry.FullName)"
        $done++
        continue
      }

      if (!(Test-Path -LiteralPath $targetDir)) {
        New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
      }
      if (Test-Path -LiteralPath $target) {
        Remove-Item -LiteralPath $target -Force
      }
      [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $target)

      $done++
      $percent = if ($total -gt 0) { [int][math]::Floor(($done * 100) / $total) } else { 100 }
      if ($percent -ge $lastPercent + 5 -or $percent -eq 100) {
        $lastPercent = $percent
        Write-Output "[$Label] Extracting $percent% ($done / $total files)"
      }
    }
  } finally {
    $archive.Dispose()
  }
}

foreach ($component in $components) {
  $marker = Join-Path $component.MarkerRoot $component.Marker
  if ((Test-Path -LiteralPath $marker) -and ((Get-Content -LiteralPath $marker -Raw).Trim() -eq $component.Version)) {
    Write-Output "[$($component.Name)] Already installed for $($component.Version); skipping"
    continue
  }

  $url = "$($component.BaseUrl)/$($component.Asset)"
  $zip = Join-Path $cache $component.Asset
  Download-FileWithProgress -Url $url -OutFile $zip -Label $component.Name
  Expand-ZipWithProgress -Zip $zip -Destination $component.Destination -Label $component.Name
  Set-Content -LiteralPath $marker -Value $component.Version -Encoding ASCII
  Remove-Item -LiteralPath $zip -Force
  Write-Output "[$($component.Name)] Complete"
}

Set-Content -LiteralPath (Join-Path $InstallDir ".runtime-root") -Value $runtimeResourcesRoot -Encoding ASCII
Remove-Item -LiteralPath $cache -Recurse -Force -ErrorAction SilentlyContinue
Write-Output "Install complete"
