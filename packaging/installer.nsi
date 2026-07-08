Unicode true

!ifndef APP_VERSION
  !define APP_VERSION "dev"
!endif
!ifndef SOURCE_DIR
  !define SOURCE_DIR "dist"
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
VIAddVersionKey "FileDescription" "Hindsight Local Manager installer"
VIAddVersionKey "FileVersion" "${APP_VERSION}"
VIAddVersionKey "ProductVersion" "${APP_VERSION}"

Section "Install"
  SetOutPath "$INSTDIR"
  File "${SOURCE_DIR}\Hindsight Local Manager.exe"

  SetOutPath "$INSTDIR\resources"
  File /r "${SOURCE_DIR}\resources\*.*"

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
