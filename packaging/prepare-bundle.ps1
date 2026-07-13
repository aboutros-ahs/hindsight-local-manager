param(
  [string]$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path,
  [string]$PythonVersion = "3.11.9",
  [string]$NodeVersion = "22.13.1",
  [switch]$SkipDownloads
)

$ErrorActionPreference = "Stop"

$resources = Join-Path $Root "resources"
$cache = Join-Path $Root ".bundle-cache"
$pythonDir = Join-Path $resources "python"
$nodeDir = Join-Path $resources "node"
$controlPlaneDir = Join-Path $resources "control-plane"

New-Item -ItemType Directory -Path $resources, $cache, $controlPlaneDir -Force | Out-Null

function Download-File($Url, $OutFile) {
  if ($SkipDownloads -and !(Test-Path -LiteralPath $OutFile)) {
    throw "Missing cached artifact: $OutFile"
  }
  if (!(Test-Path -LiteralPath $OutFile)) {
    Invoke-WebRequest -Uri $Url -OutFile $OutFile
  }
}

function Expand-ZipClean($Zip, $Destination) {
  if (Test-Path -LiteralPath $Destination) {
    Remove-Item -LiteralPath $Destination -Recurse -Force
  }
  New-Item -ItemType Directory -Path $Destination -Force | Out-Null
  Expand-Archive -LiteralPath $Zip -DestinationPath $Destination -Force
}

Write-Host "Preparing bundled resources in $resources"

$nodeZip = Join-Path $cache "node-v$NodeVersion-win-x64.zip"
Download-File "https://nodejs.org/dist/v$NodeVersion/node-v$NodeVersion-win-x64.zip" $nodeZip
$nodeExtract = Join-Path $cache "node-extract"
Expand-ZipClean $nodeZip $nodeExtract
$nodeSource = Join-Path $nodeExtract "node-v$NodeVersion-win-x64"
if (Test-Path -LiteralPath $nodeDir) { Remove-Item -LiteralPath $nodeDir -Recurse -Force }
Copy-Item -LiteralPath $nodeSource -Destination $nodeDir -Recurse

$pythonZip = Join-Path $cache "python-$PythonVersion-embed-amd64.zip"
Download-File "https://www.python.org/ftp/python/$PythonVersion/python-$PythonVersion-embed-amd64.zip" $pythonZip
Expand-ZipClean $pythonZip $pythonDir

$pth = Get-ChildItem -LiteralPath $pythonDir -Filter "python*._pth" | Select-Object -First 1
if ($pth) {
  $pthText = Get-Content -LiteralPath $pth.FullName -Raw
  if ($pthText -notmatch "(?m)^import site$") {
    $pthText = $pthText -replace "#import site", "import site"
    Set-Content -LiteralPath $pth.FullName -Value $pthText -Encoding ASCII
  }
}

$getPip = Join-Path $cache "get-pip.py"
Download-File "https://bootstrap.pypa.io/get-pip.py" $getPip
& (Join-Path $pythonDir "python.exe") $getPip
& (Join-Path $pythonDir "python.exe") -m pip install --upgrade pip
$sitePackages = Join-Path $pythonDir "Lib\site-packages"
New-Item -ItemType Directory -Path $sitePackages -Force | Out-Null
& (Join-Path $pythonDir "python.exe") -m pip install --upgrade --target $sitePackages --no-warn-script-location hindsight-api sentence-transformers
& (Join-Path $pythonDir "python.exe") -c "import hindsight_api, sentence_transformers; print('Python runtime imports OK')"

Get-ChildItem -LiteralPath $pythonDir -Recurse -Directory -Filter "__pycache__" | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
Get-ChildItem -LiteralPath $pythonDir -Recurse -Directory | Where-Object { $_.Name -in @("tests", "test") } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
Get-ChildItem -LiteralPath $pythonDir -Recurse -Include "*.pyc", "*.pyo" | Remove-Item -Force -ErrorAction SilentlyContinue
& (Join-Path $pythonDir "python.exe") -m pip cache purge

Push-Location $controlPlaneDir
try {
  if (!(Test-Path -LiteralPath "package.json")) {
    '{"private":true,"dependencies":{"@vectorize-io/hindsight-control-plane":"latest"}}' | Set-Content -LiteralPath "package.json" -Encoding ASCII
  }
  & (Join-Path $nodeDir "npm.cmd") install --omit=dev
} finally {
  Pop-Location
}

Write-Host "Bundle resources ready. Run: wails build"
