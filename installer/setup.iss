; =============================================================================
; Cronos POS Agent - Inno Setup Installer Script
; Silent-capable Windows installer (/VERYSILENT /SUPPRESSMSGBOXES /NORESTART)
;
; Build with: ISCC.exe setup.iss
; Requires:   Inno Setup 6.3+ (https://jrsoftware.org/isinfo.php)
;
; PERMANENT INSTALL LOCATION (QZ Tray style)
; ------------------------------------------
; The binary is always installed into a fixed system path, never into the
; Downloads folder and never into a temporary directory:
;
;   * Elevated install     -> C:\Program Files\CronosAgent
;   * Non-elevated install -> C:\ProgramData\CronosAgent
;
; The agent recognises both as permanent locations, so it does not relocate
; itself on startup. Runtime data (config.json, logs and certificates) lives
; apart, in %LOCALAPPDATA%\CronosAgent, because Program Files is read-only for
; the standard user who actually runs the agent.
;
; STATE PRESERVATION CONTRACT
; ---------------------------
; The uninstaller removes the program, its shortcuts and its autostart entry,
; but it never removes the files that bind this machine to the POS: config.json
; (which carries the api_token already handed to the frontend), the TLS private
; key and the certificate. See the [Code] section for how that is enforced.
;
; DIALOGS THE OPERATOR SEES
; -------------------------
; An attended install shows exactly two dialogs, and they are the whole user
; interface of this installer:
;
;   1. Before anything is written, InitializeSetup shows a welcome box on a
;      fresh machine or an upgrade box (naming both versions) when a previous
;      install is found. Cancelling there aborts with nothing touched.
;   2. When the progress bar is done, the agent itself shows the confirmation:
;      it is launched with --setup-mode=install|update, so its message matches
;      what just happened. That dialog is painted by the agent rather than by
;      Setup on purpose - it is the only confirmation that also proves the agent
;      is actually running, which is the thing the operator needs to know.
;
; A silent rollout (/VERYSILENT) shows neither: there is nobody in front of
; those screens to close them.
; =============================================================================

#define AppName "Cronos POS Agent"
#define AppVersion "1.9.0"
#define AppPublisher "Cronos SaaS"
#define AppExeName "cronos-pos-agent.exe"
#define AppFolderName "CronosAgent"
#define AppURL "https://pos-app.tech"
#define AppSupportURL "https://pos-app.tech/soporte"
#define AutostartValueName "CronosPOSAgent"
#define AutostartRunKey "Software\Microsoft\Windows\CurrentVersion\Run"
#define AppCopyright "Copyright (C) 2026 Cronos SaaS"

; AppGuid is the identity of this product for Windows. Inno derives the
; uninstall registry key from it by appending "_is1", and that key is what puts
; the program in Settings > Apps > Installed apps: without a stable AppId there
; is no uninstall entry at all, so the program cannot be found or removed from
; there. In AppId below it is written as "{{#AppGuid}" - the extra brace is how
; Inno escapes a literal "{" in a directive value.
;
; This GUID must never change. A new one would make the next installer believe
; the machine is clean, leaving the previous version installed alongside it with
; two uninstall entries and two autostart candidates.
#define AppGuid "{B7E3F4A2-9C1D-4E5F-A8B6-7D2C3E4F5A6B}"

[Setup]
AppId={{#AppGuid}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppSupportURL}
AppUpdatesURL={#AppURL}
AppCopyright={#AppCopyright}
DefaultDirName={code:PermanentInstallDir}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputDir=Output
OutputBaseFilename=CronosAgentSetup-{#AppVersion}
Compression=lzma2/ultra64
SolidCompression=yes

; ---------------------------------------------------------------------------
; "INSTALLED APPS" ENTRY (Add or remove programs)
; ---------------------------------------------------------------------------
; Uninstallable + CreateUninstallRegKey are Inno's defaults, but they are the
; two settings that decide whether this program can be removed the way Windows
; expects, so they are written out rather than assumed. Together with AppId
; above they produce the uninstall key that Settings > Apps reads, and Inno
; fills it in from the directives here: DisplayName from AppName, DisplayVersion
; from AppVersion, Publisher from AppPublisher, HelpLink from AppSupportURL,
; URLInfoAbout from AppPublisherURL, EstimatedSize from the files installed and
; DisplayIcon from UninstallDisplayIcon.
Uninstallable=yes
CreateUninstallRegKey=yes
UninstallDisplayName={#AppName}

; ---------------------------------------------------------------------------
; VERSION METADATA OF Setup.exe ITSELF
; ---------------------------------------------------------------------------
; Without these the installer ships with an empty Properties > Details tab and
; no version at all, which is the first thing an IT department looks at before
; allowing an executable onto a till - and what SmartScreen and most endpoint
; protection suites weigh too. The agent binary carries its own equivalent
; resource (versioninfo.json -> rsrc_windows_amd64.syso).
VersionInfoVersion={#AppVersion}.0
VersionInfoProductVersion={#AppVersion}.0
VersionInfoCompany={#AppPublisher}
VersionInfoProductName={#AppName}
VersionInfoDescription={#AppName} - Instalador
VersionInfoCopyright={#AppCopyright}
VersionInfoOriginalFileName=CronosAgentSetup-{#AppVersion}.exe

; ---------------------------------------------------------------------------
; ADMINISTRATOR PRIVILEGES (UAC)
; ---------------------------------------------------------------------------
; What: PrivilegesRequired=admin makes Setup request elevation through the UAC
; consent dialog before a single file is written.
;
; How: without elevation the two operations this installer depends on would
; fail late and silently - writing into C:\Program Files\CronosAgent, and
; creating the machine-visible shortcuts under the common Start Menu / Desktop.
; Requesting admin up front turns a half-finished install into a decision the
; operator makes before anything happens.
;
; PrivilegesRequiredOverridesAllowed keeps the deployment working when no
; administrator credentials are available: Inno retries without elevation and
; PermanentInstallDir falls back to C:\ProgramData\CronosAgent, which is just as
; permanent and is writable without admin. Both paths support /VERYSILENT.
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog commandline

; The binary is amd64: without this {commonpf} would resolve to
; "Program Files (x86)" instead of "Program Files".
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible

; Windows 7 SP1 is the floor: it is what app.manifest declares support for, and
; below it the agent's TLS stack and the tray behaviour are untested. Inno
; refuses the install with a clear message instead of leaving a binary that
; starts and dies.
MinVersion=6.1sp1

; Two installers running at once would race for the same binary and the same
; registry key. SetupMutex needs no cooperation from the application, unlike
; AppMutex - which is deliberately absent here, because the agent does not
; create a named mutex and declaring one it never signals would only give Inno a
; check that always passes. Closing the running agent is handled properly by
; StopRunningAgent in [Code] and by CloseApplications below.
SetupMutex=CronosPOSAgentSetup,Global\CronosPOSAgentSetup

; Installer icon: the same tuxedo cat the agent shows in the tray. The
; executable already carries it as a Win32 resource (rsrc_windows_amd64.syso),
; so pointing UninstallDisplayIcon at the .exe shows the cat under "Installed
; apps" without shipping a separate .ico next to the binary.
SetupIconFile=..\app_icon.ico
UninstallDisplayIcon={app}\{#AppExeName}
WizardStyle=modern
CreateAppDir=yes
CloseApplications=force
CloseApplicationsFilter=*.exe
RestartApplications=no
AllowCancelDuringInstall=yes

; SetupLogging is what CopyInstallLog in [Code] copies next to the binary, so
; support can always ask for the same path instead of walking the operator
; through %TEMP%.
SetupLogging=yes

; Unattended-friendly wizard: no page the operator has to read or confirm. The
; two dialogs described at the top of this file take their place.
DisableWelcomePage=yes
DisableDirPage=yes
DisableReadyPage=yes
DisableFinishedPage=yes

; ---------------------------------------------------------------------------
; CODE SIGNING (Authenticode) - infrastructure ready, disabled by default
; ---------------------------------------------------------------------------
; What: an unsigned setup triggers the Windows SmartScreen "Unknown publisher"
; warning, which on a locked-down POS machine is often a hard stop. Signing
; replaces that warning with the publisher name carried by the certificate.
;
; How: Inno Setup never stores the certificate itself. The SignTool directive
; names a Sign Tool that the build machine defines separately, so the .pfx and
; its password stay out of this repository and out of this file.
;
;   1. Define the named tool once. Either in the Inno Setup IDE
;      (Tools > Configure Sign Tools..., name it "signtool"), or per build on
;      the command line, which is what a CI pipeline should do:
;
;        ISCC.exe /Ssigntool="C:\Program Files (x86)\Windows Kits\10\bin\x64\signtool.exe $p" installer\setup.iss
;
;      $p is substituted with the parameters written in the SignTool directive
;      below; $f is substituted with the file being signed. A pipeline should
;      import the certificate into the machine store before building
;      (certutil -f -p %CERT_PASSWORD% -importpfx cert.pfx) so that /a selects
;      it automatically and no password ever reaches a command line or a log.
;
;   2. Uncomment both directives below. SignedUninstaller also signs
;      unins000.exe - otherwise the removal path keeps raising the warning that
;      the install path no longer raises.
;
;   3. Sign the agent binary too, before compiling this script, because
;      SmartScreen inspects the .exe that lands in Program Files as well:
;
;        signtool sign /a /tr http://timestamp.digicert.com /td sha256 /fd sha256 build\cronos-pos-agent.exe
;
; /tr adds an RFC 3161 timestamp and is not optional: without it every
; signature stops validating the day the certificate expires, including the
; copies already deployed on customer machines.
;
; SignTool=signtool sign /a /tr http://timestamp.digicert.com /td sha256 /fd sha256 $f
; SignedUninstaller=yes

[Languages]
Name: "spanish"; MessagesFile: "compiler:Languages\Spanish.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
; Main binary, built with -H=windowsgui -w -s.
;
; Nothing here uses the uninsneveruninstall flag on purpose: that flag protects
; files the installer itself ships, and this installer ships exactly one file -
; the executable, which must be removed. The data that has to survive
; (config.json, private-key.pem, digital-certificate.txt) is generated by the
; agent at first run, so it never enters the uninstall log and is protected by
; the [Dirs] entry plus the [Code] section instead.
Source: "..\build\{#AppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
; Marks the runtime data folder as never-uninstall, so Inno will not reclaim it
; even when it ends up empty. This is the declarative half of the preservation
; contract; PurgeDisposableData in [Code] is the half that decides file by file.
;
; Skipped on elevated installs for the same reason the [Registry] entry is:
; {localappdata} then resolves to the administrator's profile, not the
; operator's, so creating it there would only litter the wrong account. In that
; case the agent itself creates the folder under the correct profile at first
; run (it is launched below with runasoriginaluser).
Name: "{localappdata}\{#AppFolderName}"; Flags: uninsneveruninstall; \
  Check: not IsAdminInstallMode

[Icons]
; Start Menu and Desktop shortcuts. Inno records both in the uninstall log, so
; the uninstaller removes them automatically - no [UninstallDelete] entry needed.
;
; {group} and {autodesktop} follow the install mode: on an elevated install they
; resolve to the common (all users) locations, which is what a shared POS
; machine needs; on a non-elevated install they fall back to the current user.
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExeName}"
Name: "{group}\Desinstalar {#AppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"

[Run]
; Generates the SSL certificates in the data folder of the user who installs.
; runasoriginaluser is essential when Setup runs elevated: without it the token
; and the certificates would be written into the administrator's profile
; instead of the profile of the operator working the till.
Filename: "{app}\{#AppExeName}"; Parameters: "--generate-certs"; \
  Flags: runhidden waituntilterminated runasoriginaluser
; Starts the agent once the progress bar finishes. On startup the agent
; registers its own autostart entry in HKCU, using the permanent path wrapped in
; double quotes.
;
; An attended install passes --setup-mode and --setup-id: the agent starts
; normally (tray + HTTP server) and additionally opens, exactly once per run of
; this installer, the dialog that confirms what just happened - "instalado" on a
; clean machine, "actualizado" over a previous version. Without it the install
; would end with no visible signal at all, because the binary is -H=windowsgui
; and leaves nothing but a 16 px tray icon.
;
; --setup-id is what makes that "exactly once per run" precise: the agent
; records the id it confirmed, so the relaunch from the permanent location stays
; quiet while the next run of the installer - a repair over the identical
; version included - is confirmed again.
;
; runhidden is deliberately absent here: that flag is what would keep the dialog
; from ever being seen.
Filename: "{app}\{#AppExeName}"; \
  Parameters: "--setup-mode={code:SetupModeParam} --setup-id={code:SetupRunIdParam}"; \
  Flags: nowait postinstall runasoriginaluser; Check: not WizardSilent
; Silent rollout to tills (/VERYSILENT): same startup, no dialog - there is
; nobody in front of those screens to close it.
Filename: "{app}\{#AppExeName}"; \
  Flags: nowait runhidden postinstall runasoriginaluser; Check: WizardSilent

[Registry]
; Start with Windows. The path is ALWAYS wrapped in double quotes: without them
; Windows truncates the command at the first space ("C:\Program Files\..." is
; read as "C:\Program") and drops the entry silently on every reboot.
;
; Only written on non-elevated installs: when Setup runs as administrator, HKCU
; is the administrator's hive and not the operator's. In that case the agent
; registers the entry in the correct hive during its first run (launched above
; with runasoriginaluser).
;
; uninsdeletevalue removes this value at uninstall time; the elevated case is
; covered by the reg.exe call in [UninstallRun].
Root: HKCU; Subkey: "{#AutostartRunKey}"; \
  ValueType: string; ValueName: "{#AutostartValueName}"; \
  ValueData: """{app}\{#AppExeName}"""; Flags: uninsdeletevalue; \
  Check: not IsAdminInstallMode

[UninstallRun]
; Stop the agent before removing anything: Windows will not delete a running
; executable.
Filename: "taskkill"; Parameters: "/F /IM {#AppExeName}"; Flags: runhidden
; Remove the autostart value from the real operator's HKCU hive.
;
; runasoriginaluser is what makes this land in the right hive when the
; uninstaller is elevated. reg.exe is used instead of the agent's own
; --disable-autostart flag on purpose: that flag also persists "autostart":false
; into config.json, and config.json now survives the uninstall - a later
; reinstall would inherit the disabled preference and the till would come back
; up without the agent after the next reboot. Deleting the registry value
; directly cleans up the autostart without touching preserved state.
Filename: "reg"; \
  Parameters: "delete ""HKCU\{#AutostartRunKey}"" /v {#AutostartValueName} /f"; \
  Flags: runhidden runasoriginaluser

[UninstallDelete]
; Disposable files are handled by PurgeDisposableData in [Code], which works
; from an explicit allow list and therefore cannot delete a protected file by
; accident. Only the copy of the install log and the empty-folder sweep are
; left here.
;
; {app} is removed only when nothing had to be preserved inside it: dirifempty
; is a no-op while a config.json from a <= 1.3.0 install still sits next to the
; binary. The runtime data folder under {localappdata} is deliberately absent
; from this section - it is marked uninsneveruninstall in [Dirs].
Type: files; Name: "{app}\install-log.txt"
Type: files; Name: "{app}\{#AppExeName}.old"
Type: dirifempty; Name: "{app}"

[Code]
const
  // The key Inno Setup itself writes when installing. It appends the "_is1"
  // suffix; the GUID is the AppId declared above. This is the source of truth
  // for whether this machine already has the agent installed, and it is also
  // the key that Settings > Apps reads to list and remove the program.
  UninstallKeyPath =
    'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#AppGuid}_is1';

  // Maximum wait after asking for a graceful stop, and after forcing it.
  GracefulStopTimeoutMs = 4000;
  ForcedStopTimeoutMs   = 6000;
  StopPollIntervalMs    = 250;

  // FILE_ATTRIBUTE_DIRECTORY, declared locally so the script does not depend on
  // the constant being predefined by the Inno Setup version in use.
  AttrDirectory = $10;

var
  PreviousInstallFound: Boolean;
  PreviousVersion: String;
  PreviousLocation: String;
  SetupRunId: String;

// ---------------------------------------------------------------------------
// 1. Detecting the previous installation
// ---------------------------------------------------------------------------

// ReadPreviousInstall looks for the uninstall key in one registry root and, if
// it is there, records the version and the path in the globals above.
function ReadPreviousInstall(RootKey: Integer): Boolean;
var
  Version, Location: String;
begin
  Result := False;
  if not RegKeyExists(RootKey, UninstallKeyPath) then
    exit;

  if not RegQueryStringValue(RootKey, UninstallKeyPath, 'DisplayVersion', Version) then
    Version := '';
  // "Inno Setup: App Path" is the exact value the previous install left behind;
  // InstallLocation is the same data with a trailing backslash and is only used
  // as a fallback.
  if not RegQueryStringValue(RootKey, UninstallKeyPath, 'Inno Setup: App Path', Location) then
    if not RegQueryStringValue(RootKey, UninstallKeyPath, 'InstallLocation', Location) then
      Location := '';

  PreviousVersion := Version;
  PreviousLocation := RemoveBackslashUnlessRoot(Trim(Location));
  Result := True;
end;

// DetectPreviousInstall walks every root the key may have ended up in. Several
// have to be checked because the agent installs elevated (HKLM) or not (HKCU),
// and a machine can carry an install made the other way round, or by a 32-bit
// installer (the WOW6432Node view).
function DetectPreviousInstall: Boolean;
begin
  Result := ReadPreviousInstall(HKEY_LOCAL_MACHINE);
  if not Result then
    Result := ReadPreviousInstall(HKEY_CURRENT_USER);
  if not Result then
    if IsWin64 then
      Result := ReadPreviousInstall(HKLM32);
  if not Result then
    if IsWin64 then
      Result := ReadPreviousInstall(HKCU32);
end;

// ---------------------------------------------------------------------------
// 2. Parameters handed to the agent when it is launched
// ---------------------------------------------------------------------------

// SetupModeParam tells the agent which message to show when it comes up:
// "update" over a previous install, "install" on a clean machine.
function SetupModeParam(Param: String): String;
begin
  if PreviousInstallFound then
    Result := 'update'
  else
    Result := 'install';
end;

// SetupRunIdParam identifies this run of the installer. The agent stores the id
// it has already confirmed, so the dialog appears exactly once per run: the
// relaunch from the permanent location finds the id recorded and stays quiet,
// while the next run - including a repair over the identical version - carries a
// different id and is confirmed again.
function SetupRunIdParam(Param: String): String;
begin
  Result := SetupRunId;
end;

// ---------------------------------------------------------------------------
// 3. The dialog shown before anything is written
// ---------------------------------------------------------------------------

// ShowOpeningDialog states what is about to happen and lets the operator back
// out while nothing has been touched yet.
//
// Two messages, because the two situations raise different questions. On a
// clean machine the operator needs to know what this program is and that it will
// sit in the tray. On an upgrade the only real question is whether the till has
// to be paired with the POS again, so the message names both versions and
// answers it outright.
//
// SuppressibleMsgBox, not MsgBox: a rollout with /SUPPRESSMSGBOXES answers IDOK
// by itself instead of waiting forever on a screen nobody is watching. The
// WizardSilent guard above it covers /SILENT and /VERYSILENT for the same
// reason.
function ShowOpeningDialog: Boolean;
var
  Message: String;
  Answer: Integer;
begin
  Result := True;
  if WizardSilent then
    exit;

  if PreviousInstallFound then
  begin
    Message :=
      'Se actualizará el Agente de Impresión Cronos POS.' + #13#10 + #13#10 +
      'Versión instalada: ' + PreviousVersion + #13#10 +
      'Versión nueva: {#AppVersion}';
    if PreviousLocation <> '' then
      Message := Message + #13#10 + 'Ubicación: ' + PreviousLocation;
    Message := Message + #13#10 + #13#10 +
      'Se conservan el token de seguridad que ya usa tu punto de venta, los ' +
      'certificados y la configuración de tus impresoras, así que no tendrás ' +
      'que volver a vincular esta caja.' + #13#10 + #13#10 +
      'Si el agente está funcionando ahora mismo, se cerrará solo y volverá a ' +
      'arrancar ya actualizado.' + #13#10 + #13#10 +
      '¿Deseas continuar con la actualización?';
    Answer := SuppressibleMsgBox(Message, mbConfirmation, MB_OKCANCEL, IDOK);
  end
  else
  begin
    Message :=
      'Te damos la bienvenida al instalador del Agente de Impresión Cronos POS ' +
      '(versión {#AppVersion}).' + #13#10 + #13#10 +
      'Este programa permite que tu punto de venta imprima tickets en las ' +
      'impresoras conectadas a este equipo. Se ejecuta en segundo plano, ' +
      'arranca solo con el sistema y se gestiona desde el icono del gatito ' +
      'en la barra de tareas.' + #13#10 + #13#10 +
      'La instalación no requiere que configures nada: al terminar te lo ' +
      'confirmaremos con otro mensaje.' + #13#10 + #13#10 +
      '¿Deseas continuar con la instalación?';
    Answer := SuppressibleMsgBox(Message, mbInformation, MB_OKCANCEL, IDOK);
  end;

  Result := Answer = IDOK;
  if not Result then
    Log('[Cronos] El operador canceló en el diálogo inicial.');
end;

// ---------------------------------------------------------------------------
// 4. Closing the running agent (avoids the "file in use" failure)
// ---------------------------------------------------------------------------

function ExecHidden(const FileName, Params: String; var ResultCode: Integer): Boolean;
begin
  Result := Exec(FileName, Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// AgentIsRunning queries the process list. tasklist returns 0 even when it finds
// nothing, so the answer comes from "find": ERRORLEVEL 1 when the line is
// absent. If Exec itself failed, the answer is False and the install carries on:
// Inno has its own safety net underneath (CloseApplications=force).
function AgentIsRunning: Boolean;
var
  ResultCode: Integer;
begin
  Result := ExecHidden(ExpandConstant('{cmd}'),
    '/C tasklist /FI "IMAGENAME eq {#AppExeName}" /NH | find /I "{#AppExeName}" > nul',
    ResultCode) and (ResultCode = 0);
end;

function WaitUntilAgentStops(TimeoutMs: Integer): Boolean;
var
  Waited: Integer;
begin
  Waited := 0;
  while Waited < TimeoutMs do
  begin
    if not AgentIsRunning then
    begin
      Result := True;
      exit;
    end;
    Sleep(StopPollIntervalMs);
    Waited := Waited + StopPollIntervalMs;
  end;
  Result := not AgentIsRunning;
end;

// StopRunningAgent stops the live instance before the binary is overwritten.
//
// Two steps on purpose: first a taskkill WITHOUT /F, which posts WM_CLOSE and
// lets the agent run its onExit() - it shuts the HTTP server down with
// Shutdown, so no ticket is cut halfway to the spooler and the port is
// released. Only if it survives that deadline is it forced with /F. In both
// cases /T takes the children with it (the PowerShell of Get-PrintJob can hold
// an open handle).
function StopRunningAgent: Boolean;
var
  ResultCode: Integer;
  TaskKill: String;
begin
  if not AgentIsRunning then
  begin
    Log('[Cronos] No hay ninguna instancia del agente en memoria.');
    Result := True;
    exit;
  end;

  TaskKill := ExpandConstant('{sys}\taskkill.exe');

  Log('[Cronos] Agente en ejecución: se solicita el cierre ordenado.');
  ExecHidden(TaskKill, '/T /IM {#AppExeName}', ResultCode);
  Result := WaitUntilAgentStops(GracefulStopTimeoutMs);

  if not Result then
  begin
    Log('[Cronos] El cierre ordenado no bastó: se fuerza con taskkill /F.');
    ExecHidden(TaskKill, '/F /T /IM {#AppExeName}', ResultCode);
    Result := WaitUntilAgentStops(ForcedStopTimeoutMs);
  end;

  if Result then
    Log('[Cronos] Agente detenido: el binario ya puede sobrescribirse.')
  else
    Log('[Cronos] AVISO: el agente sigue en memoria tras el taskkill.');
end;

// ---------------------------------------------------------------------------
// 5. Install log kept for support
// ---------------------------------------------------------------------------

// CopyInstallLog leaves a copy of the log that SetupLogging=yes writes into
// %TEMP% next to the binary. Support always asks for the same path instead of
// walking the operator through finding a file with the date in its name inside
// their temp folder.
procedure CopyInstallLog;
var
  Source, Target: String;
begin
  Source := ExpandConstant('{log}');
  if Source = '' then
    exit;
  if not DirExists(ExpandConstant('{app}')) then
    exit;

  Target := ExpandConstant('{app}\install-log.txt');
  if FileCopy(Source, Target, False) then
    Log('[Cronos] Registro de instalación copiado a ' + Target)
  else
    Log('[Cronos] No se pudo copiar el registro de instalación a ' + Target);
end;

// ---------------------------------------------------------------------------
// 6. Inno Setup entry points
// ---------------------------------------------------------------------------

function InitializeSetup: Boolean;
begin
  // One id per run of the installer, handed to the agent so it confirms this
  // run exactly once. Seconds are enough resolution: two runs of Setup within
  // the same second cannot happen, because SetupMutex serialises them.
  SetupRunId := GetDateTimeString('yyyymmddhhnnss', #0, #0);

  PreviousInstallFound := DetectPreviousInstall;
  if PreviousInstallFound then
    Log('[Cronos] Instalación previa detectada · versión "' + PreviousVersion +
        '" · ruta "' + PreviousLocation + '".')
  else
    Log('[Cronos] Instalación nueva: no hay ninguna versión previa registrada.');

  Result := ShowOpeningDialog;
end;

// PermanentInstallDir picks the binary's final fixed path. It never depends on
// where the installer happened to be executed from.
function PermanentInstallDir(Param: String): String;
begin
  // Upgrade: the previous install's path wins. Moving it to Program Files when
  // it lived in ProgramData would leave two binaries behind and an autostart
  // entry pointing at the one that no longer gets updated.
  if PreviousInstallFound then
    if PreviousLocation <> '' then
      if DirExists(PreviousLocation) then
      begin
        Result := PreviousLocation;
        exit;
      end;

  if IsAdminInstallMode then
    Result := ExpandConstant('{commonpf}\{#AppFolderName}')
  else
    Result := ExpandConstant('{commonappdata}\{#AppFolderName}');
end;

// Closes previous instances before upgrading: Windows does not allow
// overwriting an executable that is in use.
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  if not StopRunningAgent then
    Result :=
      'No se ha podido cerrar Cronos POS Agent, que sigue en ejecución.' + #13#10 +
      'Ciérralo desde el icono del gato en la bandeja del sistema (menú ' +
      '"Salir") y vuelve a ejecutar el instalador.';
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssDone then
    CopyInstallLog;
end;

// IsProtectedDataFile reports the files that must outlive an uninstall.
//
// What: config.json holds the api_token that was already handed to the POS
// frontend, and private-key.pem / digital-certificate.txt are the TLS material
// generated for this machine. Deleting any of them turns a reinstall or an
// upgrade into a re-pairing job for whoever runs the till.
//
// How: matching is by name, lowercased, and it is deliberately wider than the
// three known files - anything carrying "token" in its name, and any .pem /
// .key / .pfx / .crt / .cer, is treated as security material too, so a future
// credential file added by the agent is protected before this script hears
// about it.
function IsProtectedDataFile(FileName: String): Boolean;
var
  Lower: String;
begin
  Lower := Lowercase(FileName);
  Result := (Lower = 'config.json') or
            (Lower = 'private-key.pem') or
            (Lower = 'digital-certificate.txt') or
            (Pos('token', Lower) > 0) or
            (Pos('.pem', Lower) > 0) or
            (Pos('.key', Lower) > 0) or
            (Pos('.pfx', Lower) > 0) or
            (Pos('.crt', Lower) > 0) or
            (Pos('.cer', Lower) > 0);
end;

// IsDisposableDataFile reports the files the uninstaller may remove: rotated
// logs, the setup-confirmation marker and the .old binary left behind by a
// self-relocation.
//
// The marker is disposable on purpose: clearing it lets a reinstall confirm to
// the operator, once again, that everything went through.
function IsDisposableDataFile(FileName: String): Boolean;
var
  Lower: String;
begin
  Lower := Lowercase(FileName);
  Result := (Copy(Lower, 1, 16) = 'cronos-agent.log') or
            (Lower = 'welcome-shown') or
            ((Length(Lower) > 4) and (Copy(Lower, Length(Lower) - 3, 4) = '.old'));
end;

// PurgeDisposableData deletes only what IsDisposableDataFile allows, inside the
// given folder, and never recurses.
//
// How the preservation guarantee holds: the decision is an allow list, not a
// deny list. A file that is neither protected nor disposable - an operator's
// backup, a support dump, a file a future agent version writes - is left in
// place. Nothing in this uninstaller can delete the data folder as a whole:
// [UninstallDelete] carries no entry for it and [Dirs] marks it
// uninsneveruninstall.
procedure PurgeDisposableData(const Dir: String);
var
  FindRec: TFindRec;
  FullPath: String;
begin
  if not DirExists(Dir) then
    Exit;

  if FindFirst(AddBackslash(Dir) + '*', FindRec) then
  begin
    try
      repeat
        if (FindRec.Attributes and AttrDirectory) = 0 then
        begin
          FullPath := AddBackslash(Dir) + FindRec.Name;
          if IsProtectedDataFile(FindRec.Name) then
            Log('Preserved (user state): ' + FullPath)
          else if IsDisposableDataFile(FindRec.Name) then
            DeleteFile(FullPath);
        end;
      until not FindNext(FindRec);
    finally
      FindClose(FindRec);
    end;
  end;
end;

// Runs the purge in both places the agent has ever kept its data: the current
// one (%LOCALAPPDATA%\CronosAgent) and, for installs <= 1.3.0, the folder next
// to the binary.
//
// usUninstall, not usPostUninstall: it has to run before Inno processes
// [UninstallDelete], otherwise the "dirifempty" sweep over {app} would still
// see the rotated logs and would leave an empty folder behind.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
  begin
    PurgeDisposableData(ExpandConstant('{localappdata}\{#AppFolderName}'));
    PurgeDisposableData(ExpandConstant('{app}'));
  end;
end;
