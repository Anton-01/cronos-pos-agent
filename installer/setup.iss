; =============================================================================
; Cronos POS Agent — Inno Setup Installer Script
; Instalador silencioso para Windows (compatible con /VERYSILENT /SUPPRESSMSGBOXES)
;
; Compilar con: ISCC.exe setup.iss
; Requiere: Inno Setup 6.x (https://jrsoftware.org/isinfo.php)
; =============================================================================

#define AppName "Cronos POS Agent"
#define AppVersion "1.2.0"
#define AppPublisher "Cronos SaaS"
#define AppExeName "cronos-pos-agent.exe"
#define AppURL "https://pos-app.tech"

[Setup]
AppId={{B7E3F4A2-9C1D-4E5F-A8B6-7D2C3E4F5A6B}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
DefaultDirName={localappdata}\CronosAgent
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputBaseFilename=CronosAgentSetup-{#AppVersion}
Compression=lzma2/ultra64
SolidCompression=yes
PrivilegesRequired=lowest
SetupIconFile=
UninstallDisplayIcon={app}\{#AppExeName}
CreateAppDir=yes
CloseApplications=force
RestartApplications=no

; Instalación silenciosa sin interfaz visible
DisableWelcomePage=yes
DisableDirPage=yes
DisableReadyPage=yes
DisableFinishedPage=yes

[Languages]
Name: "spanish"; MessagesFile: "compiler:Languages\Spanish.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
; Binario principal compilado con -H=windowsgui -w -s
Source: "..\build\{#AppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Run]
; Genera config.json con token de seguridad en el primer arranque
Filename: "{app}\{#AppExeName}"; Parameters: "--generate-certs"; Flags: runhidden waituntilterminated
; Lanza el agente en segundo plano inmediatamente después de instalar
Filename: "{app}\{#AppExeName}"; Flags: nowait runhidden postinstall

[Registry]
; Auto-arranque silencioso con Windows (HKCU = sin permisos de admin)
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
  ValueType: string; ValueName: "CronosPOSAgent"; \
  ValueData: """{app}\{#AppExeName}"""; Flags: uninsdeletevalue

[UninstallRun]
; Cierra el agente antes de desinstalar
Filename: "taskkill"; Parameters: "/F /IM {#AppExeName}"; Flags: runhidden

[UninstallDelete]
; Limpia archivos generados en runtime
Type: files; Name: "{app}\config.json"
Type: files; Name: "{app}\cronos-agent.log"
Type: files; Name: "{app}\cronos-agent.log.*"
Type: files; Name: "{app}\private-key.pem"
Type: files; Name: "{app}\digital-certificate.txt"
Type: dirifempty; Name: "{app}"

[Code]
// Cierra instancias previas antes de actualizar
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Exec('taskkill', '/F /IM {#AppExeName}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := '';
end;
