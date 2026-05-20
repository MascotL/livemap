cmake_minimum_required(VERSION 3.21)

project(wgc_capture LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_library(wgc_capture SHARED
    src/wgc_capture.cpp
)

target_include_directories(wgc_capture PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}/include
)

target_compile_definitions(wgc_capture PRIVATE
    UNICODE
    _UNICODE
    WIN32_LEAN_AND_MEAN
    NOMINMAX
)

target_link_libraries(wgc_capture PRIVATE
    d3d11
    windowsapp
)

set_target_properties(wgc_capture PROPERTIES
    OUTPUT_NAME "wgc_capture"
)
