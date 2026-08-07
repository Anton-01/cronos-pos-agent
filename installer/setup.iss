; =============================================================================
; Cronos POS Agent — Inno Setup Installer Script
; Instalador silencioso para Windows (compatible con /VERYSILENT /SUPPRESSMSGBOXES)
;
; Compilar con: ISCC.exe setup.iss
; Requiere: Inno Setup 6.3+ (https://jrsoftware.org/isinfo.php)
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
; =============================================================================

#define AppName "Cronos POS Agent"
#define AppVersion "1.6.0"
#define AppPublisher "Cronos SaaS"
#define AppExeName "cronos-pos-agent.exe"
#define AppFolderName "CronosAgent"
#define AppURL "https://pos-app.tech"

[Setup]
AppId={{B7E3F4A2-9C1D-4E5F-A8B6-7D2C3E4F5A6B}
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
Filename: "{app}\{#AppExeName}"; Parameters: "--generate-certs"; \
  Flags: runhidden waituntilterminated runasoriginaluser
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
; Cierra el agente antes de desinstalar
Filename: "taskkill"; Parameters: "/F /IM {#AppExeName}"; Flags: runhidden
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
Type: dirifempty; Name: "{app}"

[Code]
// PermanentInstallDir elige la ruta fija definitiva del binario. Nunca depende
// de dónde se ejecutó el instalador.
function PermanentInstallDir(Param: String): String;
begin
  if IsAdminInstallMode then
    Result := ExpandConstant('{commonpf}\{#AppFolderName}')
  else
    Result := ExpandConstant('{commonappdata}\{#AppFolderName}');
end;

// Cierra instancias previas antes de actualizar: Windows no permite sobrescribir
// un ejecutable en uso.
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Exec('taskkill', '/F /IM {#AppExeName}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := '';
end;
