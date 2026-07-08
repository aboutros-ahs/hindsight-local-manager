Unicode true
!include LogicLib.nsh

!ifndef APP_VERSION
  !define APP_VERSION "0.0.0"
!endif
!ifndef BUNDLE_URL
  !define BUNDLE_URL "https://github.com/aboutros-ahs/hindsight-local-manager/releases/latest"
!endif
!ifndef ASSET_BASE_URL
  !define ASSET_BASE_URL "https://github.com/aboutros-ahs/hindsight-local-manager/releases/latest/download"
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
  InitPluginsDir
  File /oname=$PLUGINSDIR\web-install.ps1 "packaging\web-install.ps1"
  DetailPrint "Downloading Hindsight Local Manager components..."
  nsExec::ExecToLog `powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\web-install.ps1" -InstallDir "$INSTDIR" -Version "${APP_VERSION}" -BaseUrl "${ASSET_BASE_URL}"`
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
