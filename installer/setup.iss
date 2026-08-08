; =============================================================================
; Cronos POS Agent — Inno Setup Installer Script
; Instalador silencioso para Windows (compatible con /VERYSILENT /SUPPRESSMSGBOXES)
;
; Compilar con: ISCC.exe setup.iss
; Requiere: Inno Setup 6.3+ (https://jrsoftware.org/isinfo.php)
;
; ATENCIÓN AL GUARDAR: este archivo va en UTF-8 CON BOM. Inno Setup 6 lee un .iss
; sin BOM usando la página ANSI del sistema, y los textos de la pantalla de
; reinstalación saldrían con la mojibake clásica ("ActualizaciÃ³n").
;
; UBICACIÓN PERMANENTE (estilo QZ Tray)
; -------------------------------------
; El binario se instala SIEMPRE en una ruta fija del sistema, nunca en Descargas
; ni en carpetas temporales:
;
;   * Instalación con privilegios de administrador -> C:\Program Files\CronosAgent
;   * Instalación sin elevación                    -> C:\ProgramData\CronosAgent
;
; Ambas son ubicaciones que el agente reconoce como permanentes, de modo que no
; se reubica a sí mismo al arrancar. Los datos de runtime (config.json, logs y
; certificados) viven aparte, en %LOCALAPPDATA%\CronosAgent, porque Program Files
; es de sólo lectura para un usuario estándar.
;
; ACTUALIZACIÓN / REINSTALACIÓN
; -----------------------------
; InitializeSetup() busca la clave de desinstalación que este mismo AppId dejó en
; el registro. Si la encuentra:
;
;   * Muestra una pantalla que explica al operador qué se va a actualizar y qué
;     se conserva (config.json, api_token, certificados y preferencias).
;   * Reutiliza la ruta de la instalación anterior como destino, para no dejar
;     dos copias del agente en el equipo.
;   * No regenera los certificados SSL: ya existen y volver a emitirlos
;     invalidaría el par que el frontend pueda tener aceptado.
;
; En /SILENT y /VERYSILENT la pantalla se omite: no hay nadie delante de la caja
; de cobro que la cierre.
;
; ARCHIVOS EN USO
; ---------------
; PrepareToInstall() detiene el agente que esté en memoria ANTES de que Inno
; copie el .exe: Windows no permite sobrescribir un ejecutable en ejecución.
; Primero se pide un cierre ordenado y sólo si sobrevive se fuerza (ver
; StopRunningAgent). Si aun así sigue vivo, la instalación se aborta con un
; mensaje claro en vez de fallar a medio copiar.
; =============================================================================

#define AppName "Cronos POS Agent"
#define AppVersion "1.6.0"
#define AppPublisher "Cronos SaaS"
#define AppExeName "cronos-pos-agent.exe"
#define AppFolderName "CronosAgent"
#define AppURL "https://pos-app.tech"
; El GUID se define aparte para poder reutilizarlo en [Code] al componer la ruta
; de la clave de desinstalación. En AppId va como "{{#AppGuid}": la llave extra
; es el escape que Inno exige para un valor que empieza por "{".
#define AppGuid "{B7E3F4A2-9C1D-4E5F-A8B6-7D2C3E4F5A6B}"

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

; Se pide elevación para instalar en C:\Program Files. Si no hay credenciales de
; administrador, Inno reintenta sin elevar y PermanentInstallDir cae a
; C:\ProgramData\CronosAgent, que también es permanente y escribible sin admin.
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog commandline

; El binario es amd64: sin esto {commonpf} apuntaría a "Program Files (x86)".
ArchitecturesInstallIn64BitMode=x64compatible

; Icono del instalador: el mismo gato tuxedo que lleva el agente en la bandeja.
; El ejecutable ya lo trae como recurso Win32 (rsrc_windows_amd64.syso), así que
; UninstallDisplayIcon apuntando al .exe muestra el gato en "Aplicaciones
; instaladas" sin necesidad de copiar el .ico junto al binario.
SetupIconFile=..\app_icon.ico
UninstallDisplayIcon={app}\{#AppExeName}
CreateAppDir=yes
CloseApplications=force
RestartApplications=no

; Al actualizar, reutiliza el directorio de la instalación anterior en vez de
; DefaultDirName. Es el comportamiento por defecto de Inno, pero se declara
; explícitamente porque aquí es un requisito: mover el binario de ProgramData a
; Program Files en una actualización dejaría dos copias y una entrada de
; auto-arranque apuntando a la vieja.
UsePreviousAppDir=yes

; Registro de eventos SIEMPRE activo (requisito de soporte técnico). Inno escribe
; un log detallado en %TEMP%\Setup Log AAAA-MM-DD #NNN.txt con cada archivo
; copiado, cada clave de registro tocada y el motivo exacto de cualquier fallo.
; Al terminar se deja además una copia junto al binario ({app}\install-log.txt)
; para que soporte no tenga que buscar en el temporal del operador. Un
; administrador puede fijar la ruta con  /LOG="C:\ruta\cronos-install.log".
SetupLogging=yes

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
; Genera los certificados SSL en el directorio de datos del usuario que instala.
; runasoriginaluser es imprescindible cuando el instalador corre elevado: sin él
; el token y los certificados se escribirían en el perfil del administrador y no
; en el del operador de la caja.
;
; Sólo en instalación NUEVA: GenerateCerts() abre los archivos con O_TRUNC, así
; que en una actualización volvería a emitir el par RSA y tumbaría el
; certificado que el frontend pudiera tener ya aceptado. La promesa de la
; pantalla de reinstalación ("no tienes que reconfigurar nada") depende de esto.
Filename: "{app}\{#AppExeName}"; Parameters: "--generate-certs"; \
  Flags: runhidden waituntilterminated runasoriginaluser; Check: not IsUpgradeInstall
; Lanza el agente al terminar la barra de progreso. Al arrancar registra por sí
; mismo el auto-arranque en HKCU con la ruta permanente entre comillas dobles.
;
; En una instalación atendida se le pasa --first-run: el agente arranca normal
; (bandeja + servidor HTTP) y además abre UNA sola vez la ventana que confirma
; al operador que la instalación ha ido bien. Sin ella la instalación termina
; sin ninguna señal visible, porque el binario es -H=windowsgui y sólo deja un
; icono de 16 px en la bandeja.
;
; Aquí no se pone runhidden: esa bandera es la que impediría que la ventana de
; bienvenida llegara a verse.
Filename: "{app}\{#AppExeName}"; Parameters: "--first-run"; \
  Flags: nowait postinstall runasoriginaluser; Check: not WizardSilent
; Despliegue silencioso en cajas de cobro (/VERYSILENT): mismo arranque, sin
; ventana de bienvenida — no hay nadie delante de la pantalla que la cierre.
Filename: "{app}\{#AppExeName}"; \
  Flags: nowait runhidden postinstall runasoriginaluser; Check: WizardSilent

[Registry]
; Auto-arranque con Windows. La ruta va SIEMPRE entre comillas dobles: sin ellas
; Windows trunca el comando en el primer espacio ("C:\Program Files\..." se lee
; como "C:\Program") e ignora la entrada silenciosamente tras cada reinicio.
;
; Sólo se escribe en instalaciones sin elevación: cuando el instalador corre como
; administrador, HKCU es la rama del administrador y no la del operador. En ese
; caso es el propio agente quien registra la entrada en la rama correcta durante
; su primer arranque (lanzado arriba con runasoriginaluser).
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
  ValueType: string; ValueName: "CronosPOSAgent"; \
  ValueData: """{app}\{#AppExeName}"""; Flags: uninsdeletevalue; \
  Check: not IsAdminInstallMode

[UninstallRun]
; Cierra el agente antes de desinstalar (árbol completo: el agente puede tener
; un powershell hijo consultando la cola de impresión)
Filename: "{sys}\taskkill.exe"; Parameters: "/F /T /IM {#AppExeName}"; Flags: runhidden
; Elimina la entrada de auto-arranque de la rama HKCU del operador real
Filename: "{app}\{#AppExeName}"; Parameters: "--disable-autostart"; \
  Flags: runhidden waituntilterminated runasoriginaluser skipifdoesntexist

[UninstallDelete]
; Datos de runtime (ubicación actual, por usuario)
Type: files; Name: "{localappdata}\{#AppFolderName}\config.json"
Type: files; Name: "{localappdata}\{#AppFolderName}\cronos-agent.log"
Type: files; Name: "{localappdata}\{#AppFolderName}\cronos-agent.log.*"
Type: files; Name: "{localappdata}\{#AppFolderName}\private-key.pem"
Type: files; Name: "{localappdata}\{#AppFolderName}\digital-certificate.txt"
; Marcador de la ventana de bienvenida: si no se borra, una reinstalación no
; volvería a confirmarle al operador que todo ha ido bien.
Type: files; Name: "{localappdata}\{#AppFolderName}\welcome-shown"
Type: dirifempty; Name: "{localappdata}\{#AppFolderName}"
; Restos de instalaciones anteriores que guardaban los datos junto al binario
Type: files; Name: "{app}\config.json"
Type: files; Name: "{app}\cronos-agent.log"
Type: files; Name: "{app}\cronos-agent.log.*"
Type: files; Name: "{app}\private-key.pem"
Type: files; Name: "{app}\digital-certificate.txt"
Type: files; Name: "{app}\{#AppExeName}.old"
; Copia del registro de instalación que deja CopyInstallLog()
Type: files; Name: "{app}\install-log.txt"
Type: dirifempty; Name: "{app}"

[Code]
const
  // Clave que el propio Inno Setup escribe al instalar. El sufijo "_is1" lo
  // añade él; el GUID es el AppId declarado arriba. Es la fuente de verdad para
  // saber si este equipo ya tiene el agente instalado.
  UninstallKeyPath =
    'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#AppGuid}_is1';

  // Espera máxima tras pedir el cierre ordenado y tras forzarlo.
  GracefulStopTimeoutMs = 4000;
  ForcedStopTimeoutMs   = 6000;
  StopPollIntervalMs    = 250;

var
  PreviousInstallFound: Boolean;
  PreviousVersion: String;
  PreviousLocation: String;

// ---------------------------------------------------------------------------
// 1. Detección de la instalación previa
// ---------------------------------------------------------------------------

// ReadPreviousInstall busca la clave de desinstalación en una rama concreta del
// registro y, si está, guarda versión y ruta en las variables globales.
function ReadPreviousInstall(RootKey: Integer): Boolean;
var
  Version, Location: String;
begin
  Result := False;
  if not RegKeyExists(RootKey, UninstallKeyPath) then
    exit;

  if not RegQueryStringValue(RootKey, UninstallKeyPath, 'DisplayVersion', Version) then
    Version := '';
  // "Inno Setup: App Path" es el valor exacto que dejó la instalación anterior;
  // InstallLocation es el mismo dato con barra final y sólo se usa de reserva.
  if not RegQueryStringValue(RootKey, UninstallKeyPath, 'Inno Setup: App Path', Location) then
    if not RegQueryStringValue(RootKey, UninstallKeyPath, 'InstallLocation', Location) then
      Location := '';

  PreviousVersion := Version;
  PreviousLocation := RemoveBackslashUnlessRoot(Trim(Location));
  Result := True;
end;

// DetectPreviousInstall recorre las ramas donde puede haber quedado la clave.
// Hay que mirar en varias porque el agente se instala con elevación (HKLM) o
// sin ella (HKCU), y una máquina puede arrastrar una instalación hecha con la
// otra modalidad o con un instalador de 32 bits (vista WOW6432Node).
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

// IsUpgradeInstall se usa desde [Run] con Check:.
function IsUpgradeInstall: Boolean;
begin
  Result := PreviousInstallFound;
end;

// ---------------------------------------------------------------------------
// 2. Pantalla de actualización / reinstalación
// ---------------------------------------------------------------------------

// NewLabel crea una etiqueta multilínea ya posicionada, para no repetir seis
// veces las mismas cinco asignaciones.
function NewLabel(Owner: TSetupForm; ALeft, ATop, AWidth, AHeight: Integer;
  const ACaption: String): TNewStaticText;
begin
  Result := TNewStaticText.Create(Owner);
  Result.Parent := Owner;
  Result.AutoSize := False;
  Result.WordWrap := True;
  Result.Left := ScaleX(ALeft);
  Result.Top := ScaleY(ATop);
  Result.Width := ScaleX(AWidth);
  Result.Height := ScaleY(AHeight);
  Result.Caption := ACaption;
end;

// ShowUpgradeNotice explica al operador qué va a pasar antes de tocar nada.
// Devuelve False si decide cancelar, y entonces InitializeSetup aborta.
function ShowUpgradeNotice: Boolean;
var
  Form: TSetupForm;
  Title, Body, Kept, Detected: TNewStaticText;
  ContinueButton, CancelButton: TNewButton;
  DetectedText: String;
begin
  Form := CreateCustomForm();
  try
    Form.Caption := 'Actualización / Reinstalación detectada de Cronos POS Agent';
    Form.BorderStyle := bsDialog;
    Form.ClientWidth := ScaleX(540);
    Form.ClientHeight := ScaleY(346);
    Form.Center;

    Title := NewLabel(Form, 24, 20, 492, 46,
      'Actualización / Reinstalación detectada de Cronos POS Agent');
    Title.Font.Size := Title.Font.Size + 3;
    Title.Font.Style := [fsBold];

    Body := NewLabel(Form, 24, 74, 492, 70,
      'Hemos detectado que Cronos POS Agent ya se encuentra instalado en este ' +
      'equipo. El proceso actualizará el motor del agente y los recursos ' +
      'visuales, manteniendo intactas tus configuraciones y tokens de ' +
      'seguridad actuales para que no tengas que reconfigurar nada.');

    Kept := NewLabel(Form, 24, 152, 492, 108,
      'Se conservan tal y como están:' + #13#10 +
      '    •  El token de seguridad (api_token) que ya usa tu punto de venta.' + #13#10 +
      '    •  config.json: puerto, orígenes CORS y página de códigos ESC/POS.' + #13#10 +
      '    •  Los certificados SSL emitidos en la instalación original.' + #13#10 +
      '    •  Tu preferencia de "Iniciar con el Sistema".' + #13#10 +
      'Si el agente está funcionando ahora mismo, se cerrará solo y volverá a ' +
      'arrancar ya actualizado.');

    DetectedText := 'Instalación encontrada en este equipo';
    if PreviousVersion <> '' then
      DetectedText := DetectedText + ' · versión ' + PreviousVersion;
    if PreviousLocation <> '' then
      DetectedText := DetectedText + #13#10 + PreviousLocation;
    DetectedText := DetectedText + #13#10 + 'Se instalará la versión {#AppVersion}.';

    Detected := NewLabel(Form, 24, 262, 492, 48, DetectedText);
    Detected.Font.Color := clGray;

    ContinueButton := TNewButton.Create(Form);
    ContinueButton.Parent := Form;
    ContinueButton.Width := ScaleX(160);
    ContinueButton.Height := ScaleY(25);
    ContinueButton.Left := Form.ClientWidth - ScaleX(160 + 8 + 100 + 24);
    ContinueButton.Top := Form.ClientHeight - ScaleY(25 + 18);
    ContinueButton.Caption := 'Actualizar ahora';
    ContinueButton.ModalResult := mrOk;
    ContinueButton.Default := True;

    CancelButton := TNewButton.Create(Form);
    CancelButton.Parent := Form;
    CancelButton.Width := ScaleX(100);
    CancelButton.Height := ScaleY(25);
    CancelButton.Left := Form.ClientWidth - ScaleX(100 + 24);
    CancelButton.Top := ContinueButton.Top;
    CancelButton.Caption := 'Cancelar';
    CancelButton.ModalResult := mrCancel;
    CancelButton.Cancel := True;

    Result := Form.ShowModal() = mrOk;
  finally
    Form.Free;
  end;
end;

// ---------------------------------------------------------------------------
// 3. Cierre del agente en memoria (evita el "archivo en uso")
// ---------------------------------------------------------------------------

function ExecHidden(const FileName, Params: String; var ResultCode: Integer): Boolean;
begin
  Result := Exec(FileName, Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// AgentIsRunning consulta la lista de procesos. tasklist devuelve 0 aunque no
// encuentre nada, así que la respuesta la da "find": ERRORLEVEL 1 si la línea
// no aparece. Si el propio Exec fallara, se responde False y la instalación
// sigue: Inno tiene debajo su propia red (CloseApplications=force).
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

// StopRunningAgent detiene la instancia viva antes de sobrescribir el binario.
//
// Dos pasos a propósito: primero un taskkill SIN /F, que manda WM_CLOSE y deja
// que el agente ejecute su onExit() —cierra el servidor HTTP con Shutdown, así
// que no corta un ticket a medio camino del spooler y libera el puerto—. Sólo
// si sobrevive a ese plazo se fuerza con /F. En ambos casos /T arrastra a los
// hijos (el powershell de Get-PrintJob puede mantener un handle abierto).
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
// 4. Registro de instalación para soporte técnico
// ---------------------------------------------------------------------------

// CopyInstallLog deja junto al binario una copia del log que SetupLogging=yes
// genera en %TEMP%. Soporte pide siempre la misma ruta en vez de hacer buscar
// al operador un archivo con la fecha en el nombre dentro de su temporal.
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
// Puntos de entrada de Inno Setup
// ---------------------------------------------------------------------------

function InitializeSetup: Boolean;
begin
  Result := True;

  PreviousInstallFound := DetectPreviousInstall;
  if not PreviousInstallFound then
  begin
    Log('[Cronos] Instalación nueva: no hay ninguna versión previa registrada.');
    exit;
  end;

  Log('[Cronos] Instalación previa detectada. Versión: "' + PreviousVersion +
      '" · Ruta: "' + PreviousLocation + '".');

  // En /SILENT y /VERYSILENT no hay nadie que pueda cerrar la pantalla: el
  // despliegue masivo en cajas de cobro se quedaría colgado esperando un clic.
  if WizardSilent then
  begin
    Log('[Cronos] Modo silencioso: se omite la pantalla de reinstalación.');
    exit;
  end;

  Result := ShowUpgradeNotice;
  if not Result then
    Log('[Cronos] El operador canceló desde la pantalla de reinstalación.');
end;

// PermanentInstallDir elige la ruta fija definitiva del binario. Nunca depende
// de dónde se ejecutó el instalador.
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

// Cierra instancias previas antes de actualizar: Windows no permite sobrescribir
// un ejecutable en uso. PrepareToInstall() se ejecuta justo antes de la copia de
// archivos, que es exactamente donde tiene que estar esta comprobación.
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
