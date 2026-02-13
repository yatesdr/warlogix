#pragma once

#include <bit>
#include <cassert>
#include <concepts>
#include <cstdint>
#include <cstring>
#include <span>
#include <string>

namespace ser
{

template <std::endian Endianess, typename T>
T to_endian(T v)
{
	if constexpr (Endianess == std::endian::native)
	{
		return v;
	}
	else
	{
		// __builtin_bswap?? is available on both GCC and Clang
		if constexpr (sizeof(T) == 1)
		{
			return v;
		}
		else if constexpr (sizeof(T) == 2)
		{
			return std::bit_cast<T>(__builtin_bswap16(std::bit_cast<uint16_t>(v)));
		}
		else if constexpr (sizeof(T) == 4)
		{
			return std::bit_cast<T>(__builtin_bswap32(std::bit_cast<uint32_t>(v)));
		}
		else if constexpr (sizeof(T) == 8)
		{
			return std::bit_cast<T>(__builtin_bswap64(std::bit_cast<uint64_t>(v)));
		}
	}
}

template <typename T>
T to_endian(T v, std::endian endian)
{
	if (endian == std::endian::little)
	{
		return to_endian<std::endian::little>(v);
	}
	else
	{
		return to_endian<std::endian::big>(v);
	}
}

enum class Type
{
	Serializer,
	Deserializer
};

// clang-format off
template <typename T>
concept Serializer = requires(T s) {
	{ s.get_type() } -> std::same_as<Type>;
	{ s.get_endianess() } -> std::same_as<std::endian>;
	{ s.has_error() } -> std::convertible_to<bool>;
	{ s.serialized_buffer() } -> std::same_as<std::span<uint8_t>>;
	{ s.write(std::declval<std::span<const uint8_t>>()) } -> std::convertible_to<bool>;
	{ s.advance(std::declval<size_t>()) } -> std::convertible_to<bool>;
};

template <typename T>
concept Deserializer = requires(T s) {
	{ s.get_type() } -> std::same_as<Type>;
	{ s.get_endianess() } -> std::same_as<std::endian>;
	{ s.has_error() } -> std::convertible_to<bool>;
	{ s.remaining_buffer() } -> std::same_as<std::span<const uint8_t>>;
	{ s.read(std::declval<std::span<uint8_t>>()) } -> std::convertible_to<bool>;
	{ s.advance(std::declval<size_t>()) } -> std::convertible_to<bool>;
};
// clang-format on

template <std::endian Endianess = std::endian::native>
class FixedBufferSerializer
{
public:
	explicit FixedBufferSerializer(std::span<uint8_t> buffer) : _buffer(buffer) {}

	Type get_type() const
	{
		return Type::Serializer;
	}

	std::endian get_endianess() const
	{
		return Endianess;
	}

	bool has_error() const
	{
		return _has_error;
	}

	std::span<uint8_t> serialized_buffer() const
	{
		return _buffer.subspan(0, _cursor);
	}

	bool write(std::span<const uint8_t> src)
	{
		if (!can_write(src.size()))
		{
			return false;
		}
		std::memcpy(_buffer.data() + _cursor, src.data(), src.size());
		_cursor += src.size();
		return true;
	}

	bool advance(size_t off)
	{
		if (!can_write(off))
		{
			return false;
		}
		_cursor += off;
		return true;
	}

	size_t get_remaining_bytes() const
	{
		return _buffer.size() - _cursor;
	}

	void reset()
	{
		_cursor = 0;
		_has_error = false;
	}

private:
	bool can_write(size_t num)
	{
		if (_has_error)
		{
			return false;
		}

		if (_cursor + num > _buffer.size())
		{
			_has_error = true;
			return false;
		}

		return true;
	}

	std::span<uint8_t> _buffer;
	size_t _cursor = 0;
	bool _has_error = false;
};

template <typename T>
bool serialize_object(Serializer auto &ser, const T &obj)
{
	static_assert(std::is_trivially_copyable_v<T>);
	return ser.write({reinterpret_cast<const uint8_t *>(&obj), sizeof(T)});
}

bool serialize(Serializer auto &ser, std::span<const uint8_t> buffer)
{
	return ser.write(buffer);
}

bool serialize(Serializer auto &ser, std::integral auto v)
{
	v = to_endian(v, ser.get_endianess());
	return serialize_object(ser, v);
}

bool serialize(Serializer auto &ser, std::floating_point auto v)
{
	v = to_endian(v, ser.get_endianess());
	return serialize_object(ser, v);
}

// This overload is so you can easily encode hard code literals (including null)
template <size_t N>
bool serialize(Serializer auto &ser, const char (&str)[N])
{
	return ser.write({reinterpret_cast<const uint8_t *>(str), N - 1});
}

bool serialize(Serializer auto &ser, const std::string &str, size_t len = 0)
{
	assert(len == 0 || len == str.size());
	return ser.write({reinterpret_cast<const uint8_t *>(str.data()), str.size()});
}

// clang-format off
template <typename T, typename S>
concept Serializable = requires(T v, S s) {
	{ serialize(s, v) } -> std::convertible_to<bool>;
};
// clang-format on

// This needs a different name or the Serializable concept will try to check with this function and
// the concept definition will become circular.
// clang-format off
template <Serializer S, typename... Args>
	requires(Serializable<Args, S> && ...)
bool serialize_multi(S &ser, Args &&...args)
{
	return (serialize(ser, std::forward<Args>(args)) && ...);
}
// clang-format on

template <std::endian Endianess = std::endian::native>
class FixedBufferDeserializer
{
public:
	explicit FixedBufferDeserializer(std::span<const uint8_t> buffer) : _buffer(buffer) {}

	Type get_type() const
	{
		return Type::Deserializer;
	}

	std::endian get_endianess() const
	{
		return Endianess;
	}

	bool has_error() const
	{
		return _has_error;
	}

	std::span<const uint8_t> remaining_buffer() const
	{
		return _buffer.subspan(_cursor);
	}

	bool read(std::span<uint8_t> dst)
	{
		if (!can_read(dst.size()))
		{
			return false;
		}
		std::memcpy(dst.data(), _buffer.data() + _cursor, dst.size());
		_cursor += dst.size();
		return true;
	}

	bool advance(size_t off)
	{
		if (!can_read(off))
		{
			return false;
		}
		_cursor += off;
		return true;
	}

	void reset()
	{
		_cursor = 0;
		_has_error = false;
	}

private:
	bool can_read(size_t num)
	{
		if (_has_error)
		{
			return false;
		}

		if (_buffer.size() - _cursor < num)
		{
			_has_error = true;
			return false;
		}

		return true;
	}

	std::span<const uint8_t> _buffer;
	size_t _cursor = 0;
	bool _has_error = false;
};

template <typename T>
bool serialize_object(Deserializer auto &ser, T &obj)
{
	static_assert(std::is_trivially_copyable_v<T>);
	return ser.read({reinterpret_cast<uint8_t *>(&obj), sizeof(T)});
}

bool serialize(Deserializer auto &ser, std::span<uint8_t> buffer)
{
	return ser.read(buffer);
}

bool serialize(Deserializer auto &ser, std::integral auto &v)
{
	const auto res = serialize_object(ser, v);
	v = to_endian(v, ser.get_endianess());
	return res;
}

bool serialize(Deserializer auto &ser, std::floating_point auto &v)
{
	const auto res = serialize_object(ser, v);
	v = to_endian(v, ser.get_endianess());
	return res;
}

bool serialize(Deserializer auto &ser, std::string &str, size_t len)
{
	str.resize(len);
	return ser.read({reinterpret_cast<uint8_t *>(str.data()), str.size()});
}

// This needs a different name or the Serializable concept will try to check with this function and
// the concept definition will become circular.
// clang-format off
template <Deserializer S, typename... Args>
	requires(Serializable<Args, S> && ...)
bool serialize_multi(S &ser, Args &&...args)
{
	return (serialize(ser, std::forward<Args>(args)) && ...);
}
// clang-format on

template <typename T>
T read(Deserializer auto &des)
{
	T v{};
	serialize(des, v);
	return v;
}

std::string read_string(Deserializer auto &des, size_t len)
{
	std::string str;
	serialize(des, str, len);
	return str;
}

}

