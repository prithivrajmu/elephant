; Inno Setup recipe. Compile on Windows after build_release.py.
; Set ReleaseDir to the Windows release folder produced by build_release.py.
#ifndef ReleaseDir
  #error ReleaseDir must point to an extracted Windows release folder.
#endif
#include AddBackslash(ReleaseDir) + "version.iss"
#ifndef Architecture
  #define Architecture "x64compatible"
#endif
[Setup]
AppId=ElephantExperienceLayer
AppName=Elephant
AppVersion={#ElephantNumericVersion}
AppPublisher=Elephant contributors
DefaultDirName={localappdata}\Elephant
DefaultGroupName=Elephant
PrivilegesRequired=lowest
ArchitecturesAllowed={#Architecture}
OutputBaseFilename=elephant-{#ElephantVersion}-setup
OutputDir=..\dist
Compression=lzma2
SolidCompression=yes
UninstallDisplayIcon={app}\bin\elephant.exe
[Files]
Source: "{#ReleaseDir}\elephant.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "{#ReleaseDir}\README.md"; DestDir: "{app}\docs"
Source: "{#ReleaseDir}\docs\*"; DestDir: "{app}\docs\docs"; Flags: recursesubdirs createallsubdirs
Source: "{#ReleaseDir}\LICENSE"; DestDir: "{app}\docs"
[Icons]
Name: "{group}\Elephant setup"; Filename: "{cmd}"; Parameters: "/k """"{app}\bin\elephant.exe"" setup --wizard"""; WorkingDir: "{userdocs}"
[Run]
Filename: "{cmd}"; Parameters: "/k """"{app}\bin\elephant.exe"" setup --wizard"""; Description: "Set up Elephant for your agent"; Flags: postinstall nowait skipifsilent unchecked
; Uninstall removes program files. It does not remove user memories.
