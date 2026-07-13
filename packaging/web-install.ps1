param(
  [Parameter(Mandatory = $true)][string]$InstallDir,
  [Parameter(Mandatory = $true)][string]$AppVersion,
  [Parameter(Mandatory = $true)][string]$AppBaseUrl,
  [Parameter(Mandatory = $true)][string]$RuntimeVersion,
  [Parameter(Mandatory = $true)][string]$RuntimeBaseUrl,
  [string]$IncludeUI = "true",
  [ValidateSet("managed", "auto")][string]$PythonMode = "managed",
  [ValidateSet("managed", "auto")][string]$NodeMode = "managed"
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
$installHash = [math]::Abs($InstallDir.ToLowerInvariant().GetHashCode())
$cache = Join-Path $env:TEMP "HindsightLocalManagerInstall-$appTag-$runtimeTag-$installHash"
$runtimeInstallRoot = Join-Path ([Environment]::GetFolderPath("LocalApplicationData")) "HLM\r"
$runtimeResourcesRoot = Join-Path $runtimeInstallRoot "resources"
New-Item -ItemType Directory -Path $InstallDir, $cache, $runtimeInstallRoot -Force | Out-Null

function Is-Truthy([string]$Value) {
  return $Value -match '^(1|true|yes|on)$'
}

function Test-PythonRuntime([string]$Python) {
  if (!$Python -or !(Test-Path -LiteralPath $Python)) { return $false }
  $code = @'
import importlib.metadata as md
import sys
if sys.version_info < (3, 11):
    raise SystemExit(2)
if md.version("hindsight-api-slim") != "0.8.4":
    raise SystemExit(3)
import hindsight_api
import sentence_transformers
'@
  $probe = Join-Path $cache "python-runtime-probe.py"
  Set-Content -LiteralPath $probe -Value $code -Encoding ASCII
  $previousNoUserSite = $env:PYTHONNOUSERSITE
  try {
    $env:PYTHONNOUSERSITE = "1"
    & $Python $probe *> $null
    return $LASTEXITCODE -eq 0
  } catch {
    return $false
  } finally {
    $env:PYTHONNOUSERSITE = $previousNoUserSite
  }
}

function Find-CompatiblePython {
  $commands = @(Get-Command python.exe -ErrorAction SilentlyContinue | ForEach-Object { $_.Source })
  $commands += @(Get-Command py.exe -ErrorAction SilentlyContinue | ForEach-Object { $_.Source })
  foreach ($command in ($commands | Where-Object { $_ } | Select-Object -Unique)) {
    if ((Split-Path -Leaf $command) -ieq "py.exe") {
      $resolved = & $command -3.11 -c "import sys; print(sys.executable)" 2>$null
      if ($LASTEXITCODE -eq 0 -and $resolved -and (Test-PythonRuntime $resolved.Trim())) { return $resolved.Trim() }
      continue
    }
    if (Test-PythonRuntime $command) { return $command }
  }
  return ""
}

function Test-NodeRuntime([string]$Node) {
  if (!$Node -or !(Test-Path -LiteralPath $Node)) { return $false }
  $version = & $Node --version 2>$null
  if ($LASTEXITCODE -ne 0 -or !$version) { return $false }
  if ($version -match '^v(\d+)\.') { return [int]$Matches[1] -ge 20 }
  return $false
}

function Find-CompatibleNode {
  foreach ($command in (Get-Command node.exe -ErrorAction SilentlyContinue | ForEach-Object { $_.Source } | Select-Object -Unique)) {
    if (Test-NodeRuntime $command) { return $command }
  }
  return ""
}

Remove-Item -LiteralPath (Join-Path $InstallDir "resources\python") -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path $InstallDir "resources\node") -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path $InstallDir "resources\control-plane") -Recurse -Force -ErrorAction SilentlyContinue

$uiEnabled = Is-Truthy $IncludeUI
$pythonExe = ""
$nodeExe = ""
if ($PythonMode -eq "auto") {
  $pythonExe = Find-CompatiblePython
  if ($pythonExe) { Write-Output "[Python runtime] Using compatible system Python: $pythonExe" }
}
if ($uiEnabled -and $NodeMode -eq "auto") {
  $nodeExe = Find-CompatibleNode
  if ($nodeExe) { Write-Output "[Node runtime] Using compatible system Node: $nodeExe" }
}

$components = @(@{ Name = "App"; Version = $appTag; BaseUrl = $AppBaseUrl; Asset = "Hindsight-Local-Manager-$appTag-app.zip"; Marker = ".app-version"; Destination = $InstallDir; MarkerRoot = $InstallDir })
if (!$pythonExe) {
  $components += @{ Name = "Python runtime"; Version = $runtimeTag; BaseUrl = $RuntimeBaseUrl; Asset = "Hindsight-Local-Manager-$runtimeTag-python.zip"; Marker = ".python-version"; Destination = $runtimeInstallRoot; MarkerRoot = $runtimeInstallRoot }
}
if ($uiEnabled) {
  if (!$nodeExe) {
    $components += @{ Name = "Node runtime"; Version = $runtimeTag; BaseUrl = $RuntimeBaseUrl; Asset = "Hindsight-Local-Manager-$runtimeTag-node.zip"; Marker = ".node-version"; Destination = $runtimeInstallRoot; MarkerRoot = $runtimeInstallRoot }
  }
  $components += @{ Name = "Hindsight UI"; Version = $runtimeTag; BaseUrl = $RuntimeBaseUrl; Asset = "Hindsight-Local-Manager-$runtimeTag-control-plane.zip"; Marker = ".control-plane-version"; Destination = $runtimeInstallRoot; MarkerRoot = $runtimeInstallRoot }
}

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

if (!$pythonExe) { $pythonExe = Join-Path $runtimeResourcesRoot "python\python.exe" }
if ($uiEnabled -and !$nodeExe) { $nodeExe = Join-Path $runtimeResourcesRoot "node\node.exe" }
$controlPlaneCli = if ($uiEnabled) { Join-Path $runtimeResourcesRoot "control-plane\node_modules\@vectorize-io\hindsight-control-plane\bin\cli.js" } else { "" }

Set-Content -LiteralPath (Join-Path $InstallDir ".runtime-root") -Value $runtimeResourcesRoot -Encoding ASCII
$runtimeConfig = @{
  resourcesRoot = $runtimeResourcesRoot
  pythonExe = $pythonExe
  nodeExe = $nodeExe
  controlPlaneCli = $controlPlaneCli
  uiInstalled = $uiEnabled
}
$runtimeConfig | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $InstallDir ".runtime-config.json") -Encoding ASCII
Remove-Item -LiteralPath $cache -Recurse -Force -ErrorAction SilentlyContinue
Write-Output "Install complete"
