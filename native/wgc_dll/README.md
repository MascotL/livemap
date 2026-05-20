# WGC DLL

这个目录只做一件事：把 `Windows.Graphics.Capture` 返回的 WinRT/D3D11 帧，
转换成 Go 可直接消费的 `BGRA` 字节流。

## 导出函数

- `WgcCreateSessionByHwnd`
- `WgcGrabFrameBGRA`
- `WgcReleaseFrame`
- `WgcDestroySession`
- `WgcLastError`

## 推荐构建方式

不需要 Visual Studio IDE。

你只需要安装：

1. `Visual Studio Build Tools 2022`
2. 工作负载：`Desktop development with C++`
3. 组件里确保有：
   - `MSVC v143`
   - `Windows 10/11 SDK`
   - `C++ CMake tools for Windows`

然后在 `x64 Native Tools Command Prompt for VS 2022` 里执行：

```powershell
cd native\wgc_dll
cmake -S . -B build -A x64
cmake --build build --config Release
```

生成的 DLL 通常在：

```text
native\wgc_dll\build\Release\wgc_capture.dll
```

把它复制到 Go 程序运行目录下即可。

## 输出位置

把编译出的 `wgc_capture.dll` 放到 Go 程序运行目录下。

Go 侧会直接加载：

```text
./wgc_capture.dll
```
