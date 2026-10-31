#ifndef StageDir
  #error StageDir must point to verified release staging
#endif
#ifndef OutputDir
  #error OutputDir is required
#endif
#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif
[Setup]
AppId={{84DE4980-2A86-4D86-9414-D326E6A3CA52}
AppName=Engelsiz
AppVersion={#AppVersion}
DefaultDirName={localappdata}\Programs\Engelsiz
DefaultGroupName=Engelsiz
PrivilegesRequired=lowest
ArchitecturesAllowed=x64os
ArchitecturesInstallIn64BitMode=x64os
MinVersion=10.0
OutputDir={#OutputDir}
OutputBaseFilename=Engelsiz-Setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
UninstallDisplayIcon={app}\engelsiz.exe
LicenseFile={#StageDir}\LICENSES\Engelsiz-LICENSE
InfoAfterFile={#StageDir}\README.txt
[Files]
Source: "{#StageDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
[Icons]
Name: "{group}\Engelsiz"; Filename: "{app}\engelsiz.exe"; WorkingDir: "{app}"
Name: "{group}\Kaldır"; Filename: "{uninstallexe}"
[Run]
Filename: "{app}\engelsiz.exe"; Description: "Engelsiz'i aç"; Flags: nowait postinstall skipifsilent
; Deliberately no [UninstallDelete]: downloaded user content/config/state is preserved.
