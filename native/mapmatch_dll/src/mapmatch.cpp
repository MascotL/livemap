#include "mapmatch.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <memory>
#include <vector>

#include <opencv2/calib3d.hpp>
#include <opencv2/core.hpp>
#include <opencv2/features2d.hpp>
#include <opencv2/imgproc.hpp>

namespace {

constexpr int kMinGoodMatches = 8;
constexpr int kMinInliers = 6;
constexpr double kLoweRatio = 0.78;
constexpr double kRansacReprojThreshold = 8.0;

struct Matcher {
    cv::Mat world_bgr;
    cv::Mat world_gray;
    cv::Mat world_mask;
    std::vector<cv::KeyPoint> world_keypoints;
    cv::Mat world_descriptors;
    cv::Ptr<cv::Feature2D> detector;
    cv::Ptr<cv::CLAHE> clahe;
};

struct SearchData {
    std::vector<cv::KeyPoint> keypoints;
    cv::Mat descriptors;
    cv::Rect bounds;
};

cv::Mat wrap_rgba(const unsigned char* data, int width, int height, int stride) {
    return cv::Mat(height, width, CV_8UC4, const_cast<unsigned char*>(data), static_cast<size_t>(stride));
}

cv::Mat alpha_mask(const cv::Mat& rgba) {
    std::vector<cv::Mat> channels;
    cv::split(rgba, channels);
    cv::Mat mask;
    cv::threshold(channels[3], mask, 8, 255, cv::THRESH_BINARY);
    return mask;
}

cv::Mat to_bgr(const cv::Mat& rgba) {
    cv::Mat bgr;
    cv::cvtColor(rgba, bgr, cv::COLOR_RGBA2BGR);
    return bgr;
}

cv::Mat to_enhanced_gray(const cv::Mat& bgr, const cv::Ptr<cv::CLAHE>& clahe) {
    cv::Mat gray;
    cv::cvtColor(bgr, gray, cv::COLOR_BGR2GRAY);
    return gray;
}

cv::Mat minimap_donut_mask(const cv::Mat& rgba) {
    cv::Mat mask = alpha_mask(rgba);
    cv::Mat donut = cv::Mat::zeros(rgba.rows, rgba.cols, CV_8U);

    const cv::Point center((rgba.cols - 1) / 2, (rgba.rows - 1) / 2);
    const int outer_radius = std::max(1, std::min(rgba.cols, rgba.rows) / 2);
    const int inner_radius = std::max(1, static_cast<int>(std::round(outer_radius * 0.10)));

    cv::circle(donut, center, outer_radius, cv::Scalar(255), cv::FILLED, cv::LINE_AA);
    cv::circle(donut, center, inner_radius, cv::Scalar(0), cv::FILLED, cv::LINE_AA);
    cv::bitwise_and(mask, donut, mask);
    return mask;
}

bool timed_out(std::chrono::steady_clock::time_point start, int timeout_ms) {
    if (timeout_ms <= 0) {
        return false;
    }
    const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now() - start
    ).count();
    return elapsed > timeout_ms;
}

SearchData build_local_search(const Matcher& matcher, int center_x, int center_y, int radius) {
    radius = std::max(16, radius);
    cv::Rect bounds(
        center_x - radius,
        center_y - radius,
        radius * 2,
        radius * 2
    );
    bounds &= cv::Rect(0, 0, matcher.world_gray.cols, matcher.world_gray.rows);

    SearchData data;
    data.bounds = bounds;
    if (bounds.empty()) {
        return data;
    }

    cv::Mat gray_roi = matcher.world_gray(bounds);
    cv::Mat mask_roi = matcher.world_mask(bounds);
    matcher.detector->detectAndCompute(gray_roi, mask_roi, data.keypoints, data.descriptors);

    for (auto& kp : data.keypoints) {
        kp.pt.x += static_cast<float>(bounds.x);
        kp.pt.y += static_cast<float>(bounds.y);
    }
    return data;
}

double combined_score(int inliers, int good_matches) {
    if (good_matches <= 0) {
        return 0.0;
    }
    const double inlier_ratio = static_cast<double>(inliers) / static_cast<double>(good_matches);
    const double inlier_volume = std::min(1.0, static_cast<double>(inliers) / 24.0);
    return std::min(1.0, inlier_ratio * 0.65 + inlier_volume * 0.35);
}

bool valid_world_point(const Matcher& matcher, cv::Point2f point) {
    const int x = static_cast<int>(std::round(point.x));
    const int y = static_cast<int>(std::round(point.y));
    if (x < 0 || y < 0 || x >= matcher.world_mask.cols || y >= matcher.world_mask.rows) {
        return false;
    }
    return matcher.world_mask.at<unsigned char>(y, x) != 0;
}

int run_feature_match(
    const Matcher& matcher,
    const unsigned char* minimap_rgba,
    int width,
    int height,
    int stride,
    const SearchData& search,
    int threshold_ppm,
    int timeout_ms,
    MapMatchResult* result
) {
    if (!result) {
        return 0;
    }
    *result = MapMatchResult{};

    const auto start = std::chrono::steady_clock::now();
    if (!minimap_rgba || width <= 1 || height <= 1 || stride < width * 4) {
        return 0;
    }
    if (search.descriptors.empty() || search.keypoints.size() < kMinGoodMatches) {
        return 1;
    }

    cv::Mat mini_rgba = wrap_rgba(minimap_rgba, width, height, stride);
    cv::Mat mini_bgr = to_bgr(mini_rgba);
    cv::Mat mini_gray = to_enhanced_gray(mini_bgr, matcher.clahe);
    cv::Mat mini_mask = minimap_donut_mask(mini_rgba);

    std::vector<cv::KeyPoint> mini_keypoints;
    cv::Mat mini_descriptors;
    matcher.detector->detectAndCompute(mini_gray, mini_mask, mini_keypoints, mini_descriptors);
    if (mini_descriptors.empty() || mini_keypoints.size() < kMinGoodMatches) {
        return 1;
    }
    if (timed_out(start, timeout_ms)) {
        result->timed_out = 1;
        return 1;
    }

    cv::BFMatcher bf(cv::NORM_L2, false);
    std::vector<std::vector<cv::DMatch>> knn_matches;
    bf.knnMatch(mini_descriptors, search.descriptors, knn_matches, 2);

    std::vector<cv::DMatch> good_matches;
    good_matches.reserve(knn_matches.size());
    for (const auto& pair : knn_matches) {
        if (pair.size() < 2) {
            continue;
        }
        const auto& best = pair[0];
        const auto& second = pair[1];
        if (best.distance < static_cast<float>(kLoweRatio * second.distance)) {
            good_matches.push_back(best);
        }
    }
    if (static_cast<int>(good_matches.size()) < kMinGoodMatches) {
        return 1;
    }
    if (timed_out(start, timeout_ms)) {
        result->timed_out = 1;
        return 1;
    }

    std::vector<cv::Point2f> src_pts;
    std::vector<cv::Point2f> dst_pts;
    src_pts.reserve(good_matches.size());
    dst_pts.reserve(good_matches.size());
    for (const auto& match : good_matches) {
        src_pts.push_back(mini_keypoints[match.queryIdx].pt);
        dst_pts.push_back(search.keypoints[match.trainIdx].pt);
    }

    cv::Mat inlier_mask;
    cv::Mat homography = cv::findHomography(src_pts, dst_pts, cv::RANSAC, kRansacReprojThreshold, inlier_mask);
    if (homography.empty()) {
        return 1;
    }

    const int inliers = cv::countNonZero(inlier_mask);
    const double threshold = static_cast<double>(threshold_ppm) / 1000000.0;
    const double score = combined_score(inliers, static_cast<int>(good_matches.size()));
    if (inliers < kMinInliers || score < threshold) {
        return 1;
    }

    std::vector<cv::Point2f> mini_center = {
        cv::Point2f(static_cast<float>(width) / 2.0f, static_cast<float>(height) / 2.0f)
    };
    std::vector<cv::Point2f> world_center;
    cv::perspectiveTransform(mini_center, world_center, homography);
    if (world_center.empty() || !valid_world_point(matcher, world_center[0])) {
        return 1;
    }

    result->found = 1;
    result->x = static_cast<int>(std::round(world_center[0].x));
    result->y = static_cast<int>(std::round(world_center[0].y));
    result->score = score;
    return 1;
}

} // namespace

extern "C" MAPMATCH_API void* MapMatchCreate(
    const unsigned char* world_rgba,
    int width,
    int height,
    int stride
) {
    try {
        if (!world_rgba || width <= 0 || height <= 0 || stride < width * 4) {
            return nullptr;
        }

        auto matcher = std::make_unique<Matcher>();
        matcher->detector = cv::SIFT::create(80000, 3, 0.03, 10.0, 1.6);
        matcher->clahe = cv::createCLAHE(3.0, cv::Size(8, 8));

        cv::Mat rgba = wrap_rgba(world_rgba, width, height, stride);
        matcher->world_bgr = to_bgr(rgba);
        matcher->world_gray = to_enhanced_gray(matcher->world_bgr, matcher->clahe);
        matcher->world_mask = alpha_mask(rgba);
        matcher->detector->detectAndCompute(
            matcher->world_gray,
            matcher->world_mask,
            matcher->world_keypoints,
            matcher->world_descriptors
        );

        if (matcher->world_descriptors.empty()) {
            return nullptr;
        }
        return matcher.release();
    } catch (...) {
        return nullptr;
    }
}

extern "C" MAPMATCH_API void MapMatchDestroy(void* handle) {
    delete static_cast<Matcher*>(handle);
}

extern "C" MAPMATCH_API int MapMatchGlobal(
    void* handle,
    const unsigned char* minimap_rgba,
    int width,
    int height,
    int stride,
    int workers,
    int threshold_ppm,
    int timeout_ms,
    MapMatchResult* result
) {
    try {
        auto* matcher = static_cast<Matcher*>(handle);
        if (!matcher) {
            return 0;
        }
        if (workers > 0) {
            cv::setNumThreads(workers);
        }

        SearchData search;
        search.keypoints = matcher->world_keypoints;
        search.descriptors = matcher->world_descriptors;
        search.bounds = cv::Rect(0, 0, matcher->world_gray.cols, matcher->world_gray.rows);
        return run_feature_match(*matcher, minimap_rgba, width, height, stride, search, threshold_ppm, timeout_ms, result);
    } catch (...) {
        return 0;
    }
}

extern "C" MAPMATCH_API int MapMatchLocal(
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
) {
    try {
        auto* matcher = static_cast<Matcher*>(handle);
        if (!matcher) {
            return 0;
        }
        if (workers > 0) {
            cv::setNumThreads(workers);
        }

        SearchData search = build_local_search(*matcher, center_x, center_y, radius);
        return run_feature_match(*matcher, minimap_rgba, width, height, stride, search, threshold_ppm, timeout_ms, result);
    } catch (...) {
        return 0;
    }
}
