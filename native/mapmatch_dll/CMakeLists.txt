cmake_minimum_required(VERSION 3.21)

project(mapmatch LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

set(OpenCV_DIR "C:/opencv/build/install" CACHE PATH "OpenCV install directory")
find_package(OpenCV REQUIRED COMPONENTS core imgproc features2d calib3d)

add_library(mapmatch SHARED
    src/mapmatch.cpp
)

target_include_directories(mapmatch PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}/include
    ${OpenCV_INCLUDE_DIRS}
)

target_compile_definitions(mapmatch PRIVATE
    WIN32_LEAN_AND_MEAN
    NOMINMAX
)

target_link_libraries(mapmatch PRIVATE
    ${OpenCV_LIBS}
)

set_target_properties(mapmatch PROPERTIES
    OUTPUT_NAME "mapmatch"
)
