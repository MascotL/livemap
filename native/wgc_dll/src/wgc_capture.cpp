#define WGC_CAPTURE_EXPORTS
#include "../include/wgc_capture.h"

#include <d3d11.h>
#include <windows.graphics.capture.interop.h>
#include <windows.graphics.directx.direct3d11.interop.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Graphics.h>
#include <winrt/Windows.Graphics.Capture.h>
#include <winrt/Windows.Graphics.DirectX.h>
#include <winrt/Windows.Graphics.DirectX.Direct3D11.h>

#include <memory>
#include <chrono>
#include <cstring>
#include <mutex>
#include <string>
#include <thread>
#include <vector>
namespace
{
    std::wstring g_last_error;
    std::mutex g_error_mutex;

    void SetLastErrorMessage(const std::wstring& message)
    {
        std::lock_guard<std::mutex> lock(g_error_mutex);
        g_last_error = message;
    }

    std::wstring HrToHex(HRESULT hr)
    {
        wchar_t buffer[32]{};
        swprintf_s(buffer, L"0x%08X", static_cast<unsigned int>(hr));
        return buffer;
    }

    void SetLastErrorFromException(const char* prefix)
    {
        try
        {
            throw;
        }
        catch (const winrt::hresult_error& ex)
        {
            SetLastErrorMessage(std::wstring(prefix, prefix + strlen(prefix)) + L": " + ex.message().c_str());
        }
        catch (...)
        {
            SetLastErrorMessage(std::wstring(prefix, prefix + strlen(prefix)) + L": unknown error");
        }
    }

    struct CaptureSession
    {
        bool initialized = false;
        HWND hwnd = nullptr;

        winrt::com_ptr<ID3D11Device> d3d_device;
        winrt::com_ptr<ID3D11DeviceContext> d3d_context;
        winrt::Windows::Graphics::DirectX::Direct3D11::IDirect3DDevice winrt_device{ nullptr };
        winrt::Windows::Graphics::Capture::GraphicsCaptureItem item{ nullptr };
        winrt::Windows::Graphics::Capture::Direct3D11CaptureFramePool frame_pool{ nullptr };
        winrt::Windows::Graphics::Capture::GraphicsCaptureSession capture_session{ nullptr };
        winrt::com_ptr<ID3D11Texture2D> staging_texture;
        uint32_t width = 0;
        uint32_t height = 0;

        ~CaptureSession()
        {
            capture_session = nullptr;
            frame_pool = nullptr;
            item = nullptr;
            winrt_device = nullptr;
            staging_texture = nullptr;
            d3d_context = nullptr;
            d3d_device = nullptr;
        }
    };

    HRESULT CreateD3DDevice(CaptureSession& session)
    {
        UINT flags = D3D11_CREATE_DEVICE_BGRA_SUPPORT;
        D3D_FEATURE_LEVEL level{};
        const D3D_DRIVER_TYPE drivers[] = { D3D_DRIVER_TYPE_HARDWARE, D3D_DRIVER_TYPE_WARP };

        for (auto driver : drivers)
        {
            HRESULT hr = D3D11CreateDevice(
                nullptr,
                driver,
                nullptr,
                flags,
                nullptr,
                0,
                D3D11_SDK_VERSION,
                session.d3d_device.put(),
                &level,
                session.d3d_context.put());
            if (SUCCEEDED(hr))
            {
                winrt::com_ptr<IDXGIDevice> dxgi_device;
                hr = session.d3d_device->QueryInterface(__uuidof(IDXGIDevice), dxgi_device.put_void());
                if (FAILED(hr))
                {
                    return hr;
                }

                winrt::com_ptr<::IInspectable> inspectable;
                hr = CreateDirect3D11DeviceFromDXGIDevice(dxgi_device.get(), inspectable.put());
                if (FAILED(hr))
                {
                    return hr;
                }

                session.winrt_device = inspectable.as<winrt::Windows::Graphics::DirectX::Direct3D11::IDirect3DDevice>();
                return S_OK;
            }
        }

        return E_FAIL;
    }

    HRESULT CreateCaptureItemForWindow(HWND hwnd, winrt::Windows::Graphics::Capture::GraphicsCaptureItem& item)
    {
        auto factory = winrt::get_activation_factory<winrt::Windows::Graphics::Capture::GraphicsCaptureItem>();
        auto interop = factory.as<IGraphicsCaptureItemInterop>();
        return interop->CreateForWindow(
            hwnd,
            winrt::guid_of<winrt::Windows::Graphics::Capture::GraphicsCaptureItem>(),
            winrt::put_abi(item));
    }

    HRESULT EnsureStagingTexture(CaptureSession& session, uint32_t width, uint32_t height)
    {
        if (session.staging_texture && session.width == width && session.height == height)
        {
            return S_OK;
        }

        session.staging_texture = nullptr;
        session.width = width;
        session.height = height;

        D3D11_TEXTURE2D_DESC desc{};
        desc.Width = width;
        desc.Height = height;
        desc.MipLevels = 1;
        desc.ArraySize = 1;
        desc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
        desc.SampleDesc.Count = 1;
        desc.Usage = D3D11_USAGE_STAGING;
        desc.CPUAccessFlags = D3D11_CPU_ACCESS_READ;

        return session.d3d_device->CreateTexture2D(&desc, nullptr, session.staging_texture.put());
    }

    HRESULT CopyFrameToCPU(CaptureSession& session, uint8_t** frame, uint32_t* width, uint32_t* height, uint32_t* stride, std::wstring& detail)
    {
        auto next_frame = session.frame_pool.TryGetNextFrame();
        if (!next_frame)
        {
            detail = L"TryGetNextFrame returned no frame";
            return HRESULT_FROM_WIN32(ERROR_RETRY);
        }

        auto surface = next_frame.Surface();
        auto access = surface.as<::Windows::Graphics::DirectX::Direct3D11::IDirect3DDxgiInterfaceAccess>();

        winrt::com_ptr<ID3D11Texture2D> texture;
        HRESULT hr = access->GetInterface(__uuidof(ID3D11Texture2D), texture.put_void());
        if (FAILED(hr))
        {
            detail = L"IDirect3DDxgiInterfaceAccess::GetInterface failed: " + HrToHex(hr);
            return hr;
        }

        D3D11_TEXTURE2D_DESC desc{};
        texture->GetDesc(&desc);

        hr = EnsureStagingTexture(session, desc.Width, desc.Height);
        if (FAILED(hr))
        {
            detail = L"EnsureStagingTexture failed: " + HrToHex(hr);
            return hr;
        }

        session.d3d_context->CopyResource(session.staging_texture.get(), texture.get());

        D3D11_MAPPED_SUBRESOURCE mapped{};
        hr = session.d3d_context->Map(session.staging_texture.get(), 0, D3D11_MAP_READ, 0, &mapped);
        if (FAILED(hr))
        {
            detail = L"ID3D11DeviceContext::Map failed: " + HrToHex(hr);
            return hr;
        }

        const uint32_t out_stride = desc.Width * 4;
        const size_t out_size = static_cast<size_t>(out_stride) * desc.Height;
        auto* buffer = new uint8_t[out_size];

        for (uint32_t y = 0; y < desc.Height; ++y)
        {
            memcpy(
                buffer + static_cast<size_t>(y) * out_stride,
                static_cast<uint8_t*>(mapped.pData) + static_cast<size_t>(y) * mapped.RowPitch,
                out_stride);
        }

        session.d3d_context->Unmap(session.staging_texture.get(), 0);

        *frame = buffer;
        *width = desc.Width;
        *height = desc.Height;
        *stride = out_stride;
        detail = L"ok";
        return S_OK;
    }
}

extern "C" int WgcCreateSessionByHwnd(uint64_t hwnd_value, void** out_session)
{
    try
    {
        winrt::init_apartment(winrt::apartment_type::multi_threaded);

        auto session = std::make_unique<CaptureSession>();
        session->hwnd = reinterpret_cast<HWND>(hwnd_value);

        HRESULT hr = CreateD3DDevice(*session);
        if (FAILED(hr))
        {
            SetLastErrorMessage(L"CreateD3DDevice failed");
            return 0;
        }

        hr = CreateCaptureItemForWindow(session->hwnd, session->item);
        if (FAILED(hr))
        {
            SetLastErrorMessage(L"CreateCaptureItemForWindow failed");
            return 0;
        }

        auto size = session->item.Size();
        session->frame_pool = winrt::Windows::Graphics::Capture::Direct3D11CaptureFramePool::CreateFreeThreaded(
            session->winrt_device,
            winrt::Windows::Graphics::DirectX::DirectXPixelFormat::B8G8R8A8UIntNormalized,
            1,
            size);
        session->capture_session = session->frame_pool.CreateCaptureSession(session->item);
        session->capture_session.IsCursorCaptureEnabled(false);
        session->capture_session.StartCapture();
        session->initialized = true;

        *out_session = session.release();
        return 1;
    }
    catch (...)
    {
        SetLastErrorFromException("WgcCreateSessionByHwnd");
        return 0;
    }
}

extern "C" int WgcGrabFrameBGRA(
    void* raw_session,
    uint8_t** frame,
    uint32_t* width,
    uint32_t* height,
    uint32_t* stride)
{
    if (!raw_session || !frame || !width || !height || !stride)
    {
        SetLastErrorMessage(L"WgcGrabFrameBGRA invalid arguments");
        return 0;
    }

    try
    {
        auto& session = *reinterpret_cast<CaptureSession*>(raw_session);
        if (!session.initialized)
        {
            SetLastErrorMessage(L"WgcGrabFrameBGRA session not initialized");
            return 0;
        }

        HRESULT last_hr = E_FAIL;
        std::wstring detail = L"unknown";
        for (int i = 0; i < 25; ++i)
        {
            HRESULT hr = CopyFrameToCPU(session, frame, width, height, stride, detail);
            if (SUCCEEDED(hr))
            {
                return 1;
            }

            last_hr = hr;
            if (hr != HRESULT_FROM_WIN32(ERROR_RETRY))
            {
                break;
            }

            std::this_thread::sleep_for(std::chrono::milliseconds(20));
        }

        if (FAILED(last_hr))
        {
            SetLastErrorMessage(L"WgcGrabFrameBGRA failed: " + detail);
            return 0;
        }
    }
    catch (...)
    {
        SetLastErrorFromException("WgcGrabFrameBGRA");
        return 0;
    }

    SetLastErrorMessage(L"WgcGrabFrameBGRA failed: unknown state");
    return 0;
}

extern "C" void WgcReleaseFrame(void* /*session*/, uint8_t* frame)
{
    delete[] frame;
}

extern "C" void WgcDestroySession(void* raw_session)
{
    auto* session = reinterpret_cast<CaptureSession*>(raw_session);
    delete session;
}

extern "C" const wchar_t* WgcLastError()
{
    std::lock_guard<std::mutex> lock(g_error_mutex);
    return g_last_error.c_str();
}
