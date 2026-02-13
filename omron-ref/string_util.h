#pragma once

#include <span>
#include <string>
#include <string_view>
#include <vector>

namespace daq
{

std::vector<std::string_view> split(std::string_view str, char delim);

// span has so many useful conversions (range, iterators, array), but the don't work implicitly if we use a template
// parameter here
std::string to_hex(std::span<const uint8_t> buffer);

template <typename T>
std::string to_hex(const T *buffer, size_t size = 1)
{
	return to_hex({reinterpret_cast<const uint8_t *>(buffer), sizeof(T) * size});
}

}

