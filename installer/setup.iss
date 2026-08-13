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
; =============================================================================

#define AppName "Cronos POS Agent"
#define AppVersion "1.6.0"
#define AppPublisher "Cronos SaaS"
#define AppExeName "cronos-pos-agent.exe"
#define AppFolderName "CronosAgent"
#define AppURL "https://pos-app.tech"
#define AutostartValueName "CronosPOSAgent"
#define AutostartRunKey "Software\Microsoft\Windows\CurrentVersion\Run"

[Setup]
AppId={{#AppGuid}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
DefaultDirName={code:PermanentInstallDir}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputBaseFilename=CronosAgentSetup-{#AppVersion}
Compression=lzma2/ultra64
SolidCompression=yes

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

; Installer icon: the same tuxedo cat the agent shows in the tray. The
; executable already carries it as a Win32 resource (rsrc_windows_amd64.syso),
; so pointing UninstallDisplayIcon at the .exe shows the cat under "Installed
; apps" without shipping a separate .ico next to the binary.
SetupIconFile=..\app_icon.ico
UninstallDisplayIcon={app}\{#AppExeName}
CreateAppDir=yes
CloseApplications=force
RestartApplications=no

; Unattended-friendly wizard: no page the operator has to read or confirm.
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
; An attended install passes --first-run: the agent starts normally (tray + HTTP
; server) and additionally opens, exactly once, the window that confirms to the
; operator that the install went through. Without it the install ends with no
; visible signal at all, because the binary is -H=windowsgui and leaves nothing
; but a 16 px tray icon.
;
; runhidden is deliberately absent here: that flag is what would keep the
; welcome window from ever being seen.
Filename: "{app}\{#AppExeName}"; Parameters: "--first-run"; \
  Flags: nowait postinstall runasoriginaluser; Check: not WizardSilent
; Silent rollout to tills (/VERYSILENT): same startup, no welcome window - there
; is nobody in front of those screens to close it.
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
; accident. Only the empty-folder sweep is left here.
;
; {app} is removed only when nothing had to be preserved inside it: dirifempty
; is a no-op while a config.json from a <= 1.3.0 install still sits next to the
; binary. The runtime data folder under {localappdata} is deliberately absent
; from this section - it is marked uninsneveruninstall in [Dirs].
Type: dirifempty; Name: "{app}"

[Code]
// FILE_ATTRIBUTE_DIRECTORY, declared locally so the script does not depend on
// the constant being predefined by the Inno Setup version in use.
const
  AttrDirectory = $10;

// PermanentInstallDir picks the binary's final fixed path. It never depends on
// where the installer happened to be executed from.
function PermanentInstallDir(Param: String): String;
begin
  // Actualización: se respeta la ruta de la instalación anterior. Mandarla a
  // Program Files cuando vivía en ProgramData dejaría dos binarios y una
  // entrada de auto-arranque apuntando al que ya no se actualiza.
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
// logs, the welcome-window marker and the .old binary left behind by a
// self-relocation.
//
// The welcome marker is disposable on purpose: clearing it lets a reinstall
// confirm to the operator, once again, that everything went through.
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
