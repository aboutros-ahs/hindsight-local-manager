param(
  [Parameter(Mandatory = $true)][string]$InstallDir,
  [Parameter(Mandatory = $true)][string]$Version,
  [Parameter(Mandatory = $true)][string]$BaseUrl
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$versionTag = if ($Version.StartsWith("v")) { $Version } else { "v$Version" }
$cache = Join-Path $env:TEMP "HindsightLocalManagerInstall-$versionTag"
New-Item -ItemType Directory -Path $InstallDir, $cache -Force | Out-Null

$components = @(
  @{ Name = "App"; Asset = "Hindsight-Local-Manager-$versionTag-app.zip"; Marker = ".app-version" },
  @{ Name = "Python runtime"; Asset = "Hindsight-Local-Manager-$versionTag-python.zip"; Marker = ".python-version" },
  @{ Name = "Node runtime"; Asset = "Hindsight-Local-Manager-$versionTag-node.zip"; Marker = ".node-version" },
  @{ Name = "Hindsight UI"; Asset = "Hindsight-Local-Manager-$versionTag-control-plane.zip"; Marker = ".control-plane-version" }
)

function Download-FileWithProgress {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [Parameter(Mandatory = $true)][string]$OutFile,
    [Parameter(Mandatory = $true)][string]$Label
  )

  Write-Output "[$Label] Starting download"
  $request = [Net.HttpWebRequest]::Create($Url)
  $request.UserAgent = "HindsightLocalManagerInstaller/$versionTag"
  $response = $request.GetResponse()
  try {
    $total = [int64]$response.ContentLength
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

foreach ($component in $components) {
  $marker = Join-Path $InstallDir $component.Marker
  if ((Test-Path -LiteralPath $marker) -and ((Get-Content -LiteralPath $marker -Raw).Trim() -eq $versionTag)) {
    Write-Output "[$($component.Name)] Already installed for $versionTag; skipping"
    continue
  }

  $url = "$BaseUrl/$($component.Asset)"
  $zip = Join-Path $cache $component.Asset
  Download-FileWithProgress -Url $url -OutFile $zip -Label $component.Name
  Write-Output "[$($component.Name)] Extracting"
  Expand-Archive -LiteralPath $zip -DestinationPath $InstallDir -Force
  Set-Content -LiteralPath $marker -Value $versionTag -Encoding ASCII
  Remove-Item -LiteralPath $zip -Force
  Write-Output "[$($component.Name)] Complete"
}

Remove-Item -LiteralPath $cache -Recurse -Force -ErrorAction SilentlyContinue
Write-Output "Install complete"
