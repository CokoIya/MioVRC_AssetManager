Unicode true
Target amd64-unicode
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WinMessages.nsh"

!define APPNAME "MioVRCA"
!define APPID "MioVRC_AssetManager"
!define EXENAME "MioVRC_AssetManager.exe"
!define DIRNAME "MioVRCA"
!define OLDNAME "VRC素材库"
; 1.6 to 1.7.1: the name on the window and the shortcuts
!define MIDNAME "MioVRC素材托管工具"
; builds between 1.7.1 and 1.7.2
!define PREVNAME "MioVRCA素材托管Tools"
!define VERSION "1.7.8"
; registry keys keep their pre-1.6 names, so installing over an older version updates it
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\VRCAssetLibrary"
!define DIRKEY "Software\VRCAssetLibrary"

Name "${APPNAME} ${VERSION}"
OutFile "${APPID}-setup-${VERSION}.exe"
; fallback only: .onInit picks a disk other than C: (the last part, MioVRCA, is added to a folder picked with "Browse")
InstallDir "$LOCALAPPDATA\Programs\${DIRNAME}"
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "${APPNAME} ${VERSION}"

VIProductVersion "1.7.8.0"
VIAddVersionKey /LANG=2052 "ProductName" "${APPNAME}"
VIAddVersionKey /LANG=2052 "FileDescription" "${APPNAME} 安装程序"
VIAddVersionKey /LANG=2052 "CompanyName" "Coko_Iya"
VIAddVersionKey /LANG=2052 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "LegalCopyright" "Coko_Iya"

!define MUI_ICON "app.ico"
!define MUI_UNICON "app.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "安装 ${APPNAME} ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "VRChat 素材管理工具。$\r$\n$\r$\n无需管理员权限，数据仅保存在本机。"
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXENAME}"
!define MUI_FINISHPAGE_RUN_TEXT "打开 ${APPNAME}"
!define MUI_FINISHPAGE_SHOWREADME "$INSTDIR\使用说明.txt"
!define MUI_FINISHPAGE_SHOWREADME_TEXT "查看使用说明"
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Var OldDir    ; where an earlier version was installed ("" if none)
Var PickDrive ; first disk other than the system one with room to spare

; ---------- is the program running? (installer and uninstaller) ----------
; $0 = "1" when a window of it is open, its process runs, or the installed exe is in use
!macro RunningFuncs UN
Function ${UN}IsRunning
  StrCpy $0 ""
  FindWindow $1 "webview" "${APPNAME}"
  ${If} $1 <> 0
    StrCpy $0 "1"
    Return
  ${EndIf}
  FindWindow $1 "webview" "${PREVNAME}"
  ${If} $1 <> 0
    StrCpy $0 "1"
    Return
  ${EndIf}
  FindWindow $1 "webview" "${MIDNAME}"
  ${If} $1 <> 0
    StrCpy $0 "1"
    Return
  ${EndIf}
  FindWindow $1 "webview" "${OLDNAME}"
  ${If} $1 <> 0
    StrCpy $0 "1"
    Return
  ${EndIf}
  nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq ${EXENAME}" /NH /FO CSV'
  Pop $1
  Pop $2
  ${If} $1 == "0"
    ; a running copy is listed by name (otherwise only a "no tasks" line)
    StrLen $4 "${EXENAME}"
    StrLen $5 $2
    IntOp $5 $5 - $4
    ${For} $6 0 $5
      StrCpy $3 $2 $4 $6
      ${If} $3 == "${EXENAME}"
        StrCpy $0 "1"
        Return
      ${EndIf}
    ${Next}
  ${EndIf}
  ReadRegStr $2 HKCU "${DIRKEY}" "InstallDir"
  ${If} $2 != ""
    ${ForEach} $3 1 2 + 1
      ${If} $3 == 1
        StrCpy $4 "$2\${EXENAME}"
      ${Else}
        StrCpy $4 "$2\${OLDNAME}.exe"
      ${EndIf}
      ${If} ${FileExists} $4
        ClearErrors
        FileOpen $5 $4 a
        ${If} ${Errors}
          StrCpy $0 "1"
          Return
        ${EndIf}
        FileClose $5
      ${EndIf}
    ${Next}
  ${EndIf}
FunctionEnd

; ask the window to close (the program saves and quits); end the process if it is still there
Function ${UN}CloseApp
  StrCpy $7 ""
  FindWindow $1 "webview" "${APPNAME}"
  ${If} $1 <> 0
    SendMessage $1 ${WM_CLOSE} 0 0 /TIMEOUT=3000
    StrCpy $7 "1"
  ${EndIf}
  FindWindow $1 "webview" "${PREVNAME}"
  ${If} $1 <> 0
    SendMessage $1 ${WM_CLOSE} 0 0 /TIMEOUT=3000
    StrCpy $7 "1"
  ${EndIf}
  FindWindow $1 "webview" "${MIDNAME}"
  ${If} $1 <> 0
    SendMessage $1 ${WM_CLOSE} 0 0 /TIMEOUT=3000
    StrCpy $7 "1"
  ${EndIf}
  FindWindow $1 "webview" "${OLDNAME}"
  ${If} $1 <> 0
    SendMessage $1 ${WM_CLOSE} 0 0 /TIMEOUT=3000
    StrCpy $7 "1"
  ${EndIf}
  ${If} $7 == "1"
    ${For} $6 1 10
      Sleep 500
      Call ${UN}IsRunning
      ${If} $0 == ""
        Return
      ${EndIf}
    ${Next}
  ${EndIf}
  nsExec::Exec 'taskkill /F /IM "${EXENAME}"'
  Pop $1
  nsExec::Exec 'taskkill /F /IM "${OLDNAME}.exe"'
  Pop $1
  Sleep 1500
FunctionEnd

; $R0: what is about to happen ("安装" / "卸载")
Function ${UN}CheckRunning
  Call ${UN}IsRunning
  ${If} $0 == ""
    Return
  ${EndIf}
  Sleep 1500 ; just updated from inside the program: it is quitting by itself
  Call ${UN}IsRunning
  ${If} $0 == ""
    Return
  ${EndIf}
  MessageBox MB_YESNO|MB_ICONQUESTION "${APPNAME} 正在运行。$\r$\n$\r$\n是否关闭并继续$R0？" /SD IDYES IDYES close
  Abort
  close:
  Call ${UN}CloseApp
  again:
  Call ${UN}IsRunning
  ${If} $0 == "1"
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "无法关闭 ${APPNAME}，请手动关闭后点击「重试」。" /SD IDCANCEL IDRETRY again
    Abort
  ${EndIf}
FunctionEnd
!macroend
!insertmacro RunningFuncs ""
!insertmacro RunningFuncs "un."

; ---------- where to install ----------
; first fixed disk other than the system one with more than 500 MB free ("" if none)
; (System calls with explicit types: FileFunc's GetDrives crashes in 64-bit installers)
Function PickOtherDrive
  StrCpy $PickDrive ""
  StrCpy $1 $WINDIR 1
  StrCpy $8 "ABDEFGHIJKLMNOPQRSTUVWXYZ"
  ${For} $6 0 24
    StrCpy $7 $8 1 $6
    ${If} $7 != $1
      System::Call 'kernel32::GetDriveTypeW(w "$7:\") i .r2'
      ${If} $2 = 3
        System::Call 'kernel32::GetDiskFreeSpaceExW(w "$7:\", *l .r3, *l .r4, *l .r5) i .r2'
        ${If} $2 <> 0
          System::Int64Op $3 > 524288000
          Pop $2
          ${If} $2 = 1
            StrCpy $PickDrive "$7:\"
            Return
          ${EndIf}
        ${EndIf}
      ${EndIf}
    ${EndIf}
  ${Next}
FunctionEnd

Function .onInit
  StrCpy $R0 "安装"
  Call CheckRunning
  ReadRegStr $OldDir HKCU "${DIRKEY}" "InstallDir"
  ; an earlier install in a folder the player picked: install over it
  ${If} $OldDir != ""
  ${AndIf} ${FileExists} "$OldDir\*.*"
    StrLen $1 $LOCALAPPDATA
    StrCpy $2 $OldDir $1
    ${If} $2 != $LOCALAPPDATA
      StrCpy $INSTDIR $OldDir
      Return
    ${EndIf}
  ${EndIf}
  ; otherwise a disk other than C:, e.g. D:\MioVRCA
  Call PickOtherDrive
  ${If} $PickDrive != ""
    StrCpy $INSTDIR "$PickDrive${DIRNAME}"
    Return
  ${EndIf}
  ; only one disk: C:\MioVRCA when a folder can be made there
  StrCpy $1 $WINDIR 3
  ${If} ${FileExists} "$1${DIRNAME}\*.*"
    StrCpy $INSTDIR "$1${DIRNAME}"
    Return
  ${EndIf}
  ClearErrors
  CreateDirectory "$1${DIRNAME}"
  ${IfNot} ${Errors}
    RMDir "$1${DIRNAME}"
    StrCpy $INSTDIR "$1${DIRNAME}"
  ${EndIf}
FunctionEnd

Function un.onInit
  StrCpy $R0 "卸载"
  Call un.CheckRunning
FunctionEnd

Section "Install"
  SetShellVarContext current
  StrCpy $R0 "安装"
  Call CheckRunning
  SetOutPath "$INSTDIR"
  SetOverwrite on
  File "${EXENAME}"
  File "使用说明.txt"
  WriteUninstaller "$INSTDIR\卸载.exe"

  ; versions before 1.6 were called ${OLDNAME}
  Delete "$INSTDIR\${OLDNAME}.exe"
  Delete "$INSTDIR\*.exe.old"
  Delete "$DESKTOP\${OLDNAME}.lnk"
  Delete "$SMPROGRAMS\${OLDNAME}\${OLDNAME}.lnk"
  Delete "$SMPROGRAMS\${OLDNAME}\使用说明.lnk"
  Delete "$SMPROGRAMS\${OLDNAME}\卸载 ${OLDNAME}.lnk"
  RMDir "$SMPROGRAMS\${OLDNAME}"
  ; 1.6 to 1.7.1 were called ${MIDNAME}, builds after that ${PREVNAME}: their shortcuts make way for the ones below
  Delete "$DESKTOP\${MIDNAME}.lnk"
  Delete "$SMPROGRAMS\${MIDNAME}\${MIDNAME}.lnk"
  Delete "$SMPROGRAMS\${MIDNAME}\使用说明.lnk"
  Delete "$SMPROGRAMS\${MIDNAME}\卸载 ${MIDNAME}.lnk"
  RMDir "$SMPROGRAMS\${MIDNAME}"
  Delete "$DESKTOP\${PREVNAME}.lnk"
  Delete "$SMPROGRAMS\${PREVNAME}\${PREVNAME}.lnk"
  Delete "$SMPROGRAMS\${PREVNAME}\使用说明.lnk"
  Delete "$SMPROGRAMS\${PREVNAME}\卸载 ${PREVNAME}.lnk"
  RMDir "$SMPROGRAMS\${PREVNAME}"
  ; installed somewhere else before: take the old copy away (only its own files)
  ${If} $OldDir != ""
  ${AndIf} $OldDir != $INSTDIR
    Delete "$OldDir\${EXENAME}"
    Delete "$OldDir\${OLDNAME}.exe"
    Delete "$OldDir\*.exe.old"
    Delete "$OldDir\使用说明.txt"
    Delete "$OldDir\卸载.exe"
    RMDir "$OldDir"
  ${EndIf}

  CreateDirectory "$SMPROGRAMS\${APPNAME}"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk" "$INSTDIR\${EXENAME}" "" "$INSTDIR\${EXENAME}" 0
  CreateShortcut "$SMPROGRAMS\${APPNAME}\使用说明.lnk" "$INSTDIR\使用说明.txt"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\卸载 ${APPNAME}.lnk" "$INSTDIR\卸载.exe"
  CreateShortcut "$DESKTOP\${APPNAME}.lnk" "$INSTDIR\${EXENAME}" "" "$INSTDIR\${EXENAME}" 0
  ; the exe was replaced: Explorer reads its icon again (taskbar pin, shortcuts) instead of keeping a stale or blank one
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0x1000, p 0, p 0)'

  WriteRegStr HKCU "${DIRKEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "Coko_Iya"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\${EXENAME}"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\卸载.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "URLInfoAbout" "https://miovrc.com/vrca/"
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" 15000
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  Delete "$INSTDIR\${EXENAME}"
  Delete "$INSTDIR\${OLDNAME}.exe"
  Delete "$INSTDIR\*.exe.old"
  Delete "$INSTDIR\使用说明.txt"
  Delete "$INSTDIR\卸载.exe"
  RMDir "$INSTDIR"
  Delete "$DESKTOP\${APPNAME}.lnk"
  Delete "$DESKTOP\${OLDNAME}.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\使用说明.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\卸载 ${APPNAME}.lnk"
  RMDir "$SMPROGRAMS\${APPNAME}"
  Delete "$SMPROGRAMS\${OLDNAME}\${OLDNAME}.lnk"
  Delete "$SMPROGRAMS\${OLDNAME}\使用说明.lnk"
  Delete "$SMPROGRAMS\${OLDNAME}\卸载 ${OLDNAME}.lnk"
  RMDir "$SMPROGRAMS\${OLDNAME}"
  Delete "$DESKTOP\${MIDNAME}.lnk"
  Delete "$SMPROGRAMS\${MIDNAME}\${MIDNAME}.lnk"
  Delete "$SMPROGRAMS\${MIDNAME}\使用说明.lnk"
  Delete "$SMPROGRAMS\${MIDNAME}\卸载 ${MIDNAME}.lnk"
  RMDir "$SMPROGRAMS\${MIDNAME}"
  Delete "$DESKTOP\${PREVNAME}.lnk"
  Delete "$SMPROGRAMS\${PREVNAME}\${PREVNAME}.lnk"
  Delete "$SMPROGRAMS\${PREVNAME}\使用说明.lnk"
  Delete "$SMPROGRAMS\${PREVNAME}\卸载 ${PREVNAME}.lnk"
  RMDir "$SMPROGRAMS\${PREVNAME}"
  DeleteRegKey HKCU "${UNINSTKEY}"
  DeleteRegKey HKCU "${DIRKEY}"
  IfFileExists "$LOCALAPPDATA\${APPID}\library.json" askdata
  IfFileExists "$LOCALAPPDATA\${OLDNAME}\library.json" askdata done
  askdata:
    ; downloads made before any asset folder was set went into the data folder: say that they go too
    StrCpy $1 ""
    ${If} ${FileExists} "$LOCALAPPDATA\${APPID}\downloads\*.*"
    ${OrIf} ${FileExists} "$LOCALAPPDATA\${OLDNAME}\downloads\*.*"
      StrCpy $1 "$\r$\n$\r$\n注意：数据文件夹内的「downloads」文件夹（未设置下载位置时使用的默认下载文件夹）及其中已下载的素材文件也将一并删除。"
    ${EndIf}
    MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "是否同时删除素材库数据？$\r$\n（网盘链接、备注、Booth 已购记录和封面缓存）$1$\r$\n$\r$\n选择「否」将保留数据，重新安装后可继续使用。" /SD IDNO IDNO done
    RMDir /r "$LOCALAPPDATA\${APPID}"
    RMDir /r "$LOCALAPPDATA\${OLDNAME}"
  done:
SectionEnd
