#pragma once

#ifdef _WIN32
#define MAPMATCH_API __declspec(dllexport)
#else
#define MAPMATCH_API
#endif

#ifdef __cplusplus
extern "C" {
#endif

typedef struct MapMatchResult {
    int found;
    int timed_out;
    int x;
    int y;
    double score;
} MapMatchResult;

MAPMATCH_API void* MapMatchCreate(
    const unsigned char* world_rgba,
    int width,
    int height,
    int stride
);

MAPMATCH_API void MapMatchDestroy(void* handle);

MAPMATCH_API int MapMatchGlobal(
    void* handle,
    const unsigned char* minimap_rgba,
    int width,
    int height,
    int stride,
    int workers,
    int threshold_ppm,
    int timeout_ms,
    MapMatchResult* result
);

MAPMATCH_API int MapMatchLocal(
    void* handle,
    const unsigned char* minimap_rgba,
    int width,
    int height,
    int stride,
    int center_x,
    int center_y,
    int radius,
    int workers,
    int threshold_ppm,
    int timeout_ms,
    MapMatchResult* result
);

#ifdef __cplusplus
}
#endif
