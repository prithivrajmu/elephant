; Inno Setup recipe. Compile on Windows after build_release.py.
; ISCC /DReleaseDir="C:\releases\elephant-0.4.0-pilot-windows-amd64" elephant.iss
#ifndef ReleaseDir
  #error ReleaseDir must point to an extracted Windows release folder.
#endif
#ifndef Architecture
  #define Architecture "x64compatible"
#endif
[Setup]
AppId=ElephantExperienceLayer
AppName=Elephant
AppVersion=0.4.0
AppPublisher=Elephant contributors
DefaultDirName={localappdata}\Elephant
DefaultGroupName=Elephant
PrivilegesRequired=lowest
ArchitecturesAllowed={#Architecture}
OutputBaseFilename=elephant-0.4.0-pilot-setup
OutputDir=..\dist
Compression=lzma2
SolidCompression=yes
UninstallDisplayIcon={app}\bin\elephant.exe
[Files]
Source: "{#ReleaseDir}\elephant.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "{#ReleaseDir}\QUICKSTART.md"; DestDir: "{app}\docs"
Source: "{#ReleaseDir}\LANGUAGE_POLICY.md"; DestDir: "{app}\docs"
Source: "{#ReleaseDir}\LICENSE"; DestDir: "{app}\docs"
[Icons]
Name: "{group}\Elephant setup"; Filename: "{cmd}"; Parameters: "/k """"{app}\bin\elephant.exe"" setup --wizard"""; WorkingDir: "{userdocs}"
[Run]
Filename: "{cmd}"; Parameters: "/k """"{app}\bin\elephant.exe"" setup --wizard"""; Description: "Set up Elephant for your agent"; Flags: postinstall nowait skipifsilent unchecked
; Uninstall removes program files. It does not remove user memories.
