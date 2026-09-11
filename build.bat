@echo off
set "PATH=C:\Users\Deepu\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin;C:\Program Files\Go\bin;%PATH%"
echo Building entire-graph.exe with 64-bit MinGW GCC...
go build -o entire-graph.exe ./cmd/entire-graph
if %ERRORLEVEL% EQU 0 (
    echo [SUCCESS] entire-graph.exe built successfully!
) else (
    echo [ERROR] Build failed.
)
