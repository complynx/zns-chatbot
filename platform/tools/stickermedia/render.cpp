#include <rlottie.h>
#include <charconv>
#include <cstdint>
#include <iostream>
#include <iterator>
#include <string>
#include <vector>

// Receives validated, bounded vector-only JSON on stdin and a relative frame index.
// Writes premultiplied RGBA pixels. No paths or URLs are accepted.
int main(int argc, char **argv) {
    if (argc != 2) return 2;
    const std::string argument(argv[1]);
    std::size_t frame = 0;
    const auto parsed = std::from_chars(argument.data(), argument.data() + argument.size(), frame);
    if (parsed.ec != std::errc{} || parsed.ptr != argument.data() + argument.size()) return 2;
    std::string json;
    char byte = 0;
    while (std::cin.get(byte)) {
        if (json.size() >= 2U * 1024U * 1024U) return 2;
        json.push_back(byte);
    }
    auto animation = rlottie::Animation::loadFromData(json, "sticker", "/nonexistent", false);
    if (!animation || frame >= animation->totalFrame()) return 2;
    std::size_t width = 0, height = 0;
    animation->size(width, height);
    if (width == 0 || height == 0 || width > 512 || height > 512) return 2;
    std::vector<std::uint32_t> pixels(width * height);
    rlottie::Surface surface(pixels.data(), width, height, width * 4);
    animation->renderSync(frame, surface);
    for (const auto pixel : pixels) {
        const char rgba[] = {
            static_cast<char>((pixel >> 16U) & 255U),
            static_cast<char>((pixel >> 8U) & 255U),
            static_cast<char>(pixel & 255U),
            static_cast<char>((pixel >> 24U) & 255U)
        };
        std::cout.write(rgba, 4);
    }
    return std::cout ? 0 : 3;
}
