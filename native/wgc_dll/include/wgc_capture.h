#pragma once

#include <cstdint>

#ifdef WGC_CAPTURE_EXPORTS
#define WGC_CAPTURE_API __declspec(dllexport)
#else
#define WGC_CAPTURE_API __declspec(dllimport)
#endif

extern "C" {

WGC_CAPTURE_API int WgcCreateSessionByHwnd(uint64_t hwnd, void** session);
WGC_CAPTURE_API int WgcGrabFrameBGRA(
    void* session,
    uint8_t** frame,
    uint32_t* width,
    uint32_t* height,
    uint32_t* stride
);
WGC_CAPTURE_API void WgcReleaseFrame(void* session, uint8_t* frame);
WGC_CAPTURE_API void WgcDestroySession(void* session);
WGC_CAPTURE_API const wchar_t* WgcLastError();

}
