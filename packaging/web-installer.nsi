Unicode true
!include LogicLib.nsh

!ifndef APP_VERSION
  !define APP_VERSION "0.0.0"
!endif
!ifndef BUNDLE_URL
  !define BUNDLE_URL "https://github.com/aboutros-ahs/hindsight-local-manager/releases/latest"
!endif
!ifndef APP_ASSET_BASE_URL
  !define APP_ASSET_BASE_URL "https://github.com/aboutros-ahs/hindsight-local-manager/releases/latest/download"
!endif
!ifndef RUNTIME_VERSION
  !define RUNTIME_VERSION "v0.1.7"
!endif
!ifndef RUNTIME_ASSET_BASE_URL
  !define RUNTIME_ASSET_BASE_URL "https://github.com/aboutros-ahs/hindsight-local-manager/releases/download/${RUNTIME_VERSION}"
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
  File /oname=$PLUGINSDIR\web-install.ps1 "web-install.ps1"
  DetailPrint "Downloading Hindsight Local Manager components..."
  nsExec::ExecToLog `powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\web-install.ps1" -InstallDir "$INSTDIR" -AppVersion "${APP_VERSION}" -AppBaseUrl "${APP_ASSET_BASE_URL}" -RuntimeVersion "${RUNTIME_VERSION}" -RuntimeBaseUrl "${RUNTIME_ASSET_BASE_URL}"`
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
  Delete "$INSTDIR\.runtime-root"
  RMDir /r "$INSTDIR\resources"
  RMDir "$INSTDIR"
  RMDir /r "$LOCALAPPDATA\HLM\r"
  RMDir "$LOCALAPPDATA\HLM"
SectionEnd
