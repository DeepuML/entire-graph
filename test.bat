@echo off
set "PATH=C:\Users\Deepu\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin;C:\Program Files\Go\bin;%PATH%"
echo Running Buildathon Track 2 Comprehensive Test Suite...
go test -v ./internal/cli -run TestBuildathonTrack2ComprehensiveSuite
if %ERRORLEVEL% EQU 0 (
    echo [SUCCESS] All Buildathon Track 2 tests passed!
) else (
    echo [ERROR] Tests failed.
)
