Unicode true
!include LogicLib.nsh

!ifndef APP_VERSION
  !define APP_VERSION "0.0.0"
!endif
!ifndef BUNDLE_URL
  !define BUNDLE_URL "https://github.com/aboutros-ahs/hindsight-local-manager/releases/latest"
!endif
!ifndef OUT_FILE
  !define OUT_FILE "Hindsight-Local-Manager-installer.exe"
!endif

Name "Hindsight Local Manager"
OutFile "${OUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\Hindsight Local Manager"
RequestExecutionLevel user
SetCompressor /SOLID lzma
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey "ProductName" "Hindsight Local Manager"
VIAddVersionKey "CompanyName" "Alex Boutros"
VIAddVersionKey "FileDescription" "Hindsight Local Manager web installer"
VIAddVersionKey "FileVersion" "${APP_VERSION}"
VIAddVersionKey "ProductVersion" "${APP_VERSION}"

Section "Install"
  SetOutPath "$INSTDIR"
  DetailPrint "Downloading Hindsight Local Manager bundle..."
  nsExec::ExecToLog 'powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$ErrorActionPreference = ''Stop''; [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; $zip = Join-Path $env:TEMP ''Hindsight-Local-Manager-${APP_VERSION}-windows-amd64.zip''; $dest = ''$INSTDIR''; New-Item -ItemType Directory -Path $dest -Force | Out-Null; Invoke-WebRequest -Uri ''${BUNDLE_URL}'' -OutFile $zip; Get-ChildItem -LiteralPath $dest -Force | Where-Object { $_.Name -ne ''Uninstall.exe'' } | Remove-Item -Recurse -Force; Expand-Archive -LiteralPath $zip -DestinationPath $dest -Force; Remove-Item -LiteralPath $zip -Force"'
  Pop $0
  ${If} $0 != 0
    Abort "Bundle download or extraction failed. Check your internet connection and try again."
  ${EndIf}

  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\Hindsight Local Manager"
  CreateShortcut "$SMPROGRAMS\Hindsight Local Manager\Hindsight Local Manager.lnk" "$INSTDIR\Hindsight Local Manager.exe"
SectionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\Hindsight Local Manager\Hindsight Local Manager.lnk"
  RMDir "$SMPROGRAMS\Hindsight Local Manager"
  Delete "$INSTDIR\Hindsight Local Manager.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir /r "$INSTDIR\resources"
  RMDir "$INSTDIR"
SectionEnd
