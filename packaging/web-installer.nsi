Unicode true
!include LogicLib.nsh
!include Sections.nsh

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

InstType "Recommended"
InstType "Lightweight API only"
InstType "Portable"

ComponentText "Choose which Hindsight Local Manager components to install. Recommended reuses compatible system runtimes when available and falls back to managed runtimes automatically."
DirText "Choose where to install the Hindsight Local Manager application. Managed runtimes are stored separately under your local app data folder."

Page components
Page directory
Page instfiles

Var IncludeUI
Var PythonMode
Var NodeMode

VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey "ProductName" "Hindsight Local Manager"
VIAddVersionKey "CompanyName" "Alex Boutros"
VIAddVersionKey "FileDescription" "Hindsight Local Manager web installer"
VIAddVersionKey "FileVersion" "${APP_VERSION}"
VIAddVersionKey "ProductVersion" "${APP_VERSION}"

Section "Core app and Hindsight API (required)" SecCore
  SectionIn RO
SectionEnd

Section "Hindsight UI (adds Node + UI runtime)" SecUI
  SectionIn 1 3
SectionEnd

Section "Reuse compatible system Python if found" SecSystemPython
  SectionIn 1 2
SectionEnd

Section "Reuse compatible system Node if found" SecSystemNode
  SectionIn 1
SectionEnd

Section "-Install"
  SetOutPath "$INSTDIR"
  InitPluginsDir
  File /oname=$PLUGINSDIR\web-install.ps1 "web-install.ps1"
  StrCpy $IncludeUI "false"
  StrCpy $PythonMode "managed"
  StrCpy $NodeMode "managed"
  SectionGetFlags ${SecUI} $0
  IntOp $1 $0 & ${SF_SELECTED}
  ${If} $1 <> 0
    StrCpy $IncludeUI "true"
  ${EndIf}
  SectionGetFlags ${SecSystemPython} $0
  IntOp $1 $0 & ${SF_SELECTED}
  ${If} $1 <> 0
    StrCpy $PythonMode "auto"
  ${EndIf}
  SectionGetFlags ${SecSystemNode} $0
  IntOp $1 $0 & ${SF_SELECTED}
  ${If} $1 <> 0
    StrCpy $NodeMode "auto"
  ${EndIf}
  DetailPrint "Downloading Hindsight Local Manager components..."
  nsExec::ExecToLog `powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\web-install.ps1" -InstallDir "$INSTDIR" -AppVersion "${APP_VERSION}" -AppBaseUrl "${APP_ASSET_BASE_URL}" -RuntimeVersion "${RUNTIME_VERSION}" -RuntimeBaseUrl "${RUNTIME_ASSET_BASE_URL}" -IncludeUI "$IncludeUI" -PythonMode "$PythonMode" -NodeMode "$NodeMode"`
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
  Delete "$INSTDIR\.runtime-config.json"
  Delete "$INSTDIR\.app-version"
  RMDir /r "$INSTDIR\resources"
  RMDir "$INSTDIR"
  RMDir /r "$LOCALAPPDATA\HLM\r"
  RMDir "$LOCALAPPDATA\HLM"
SectionEnd
