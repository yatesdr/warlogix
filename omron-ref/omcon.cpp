#include "omron.h"

#include <cassert>
#include <optional>

#include <spdlog/fmt/fmt.h>
#include <spdlog/fmt/std.h>

#include "log.h"
#include "string_util.h"

namespace daq
{

std::string to_string(DataType type)
{
	switch (type)
	{
		case DataType::Undefined:
			return "UNDEFINED";
		case DataType::Date:
			return "DATE";
		case DataType::Time:
			return "TIME";
		case DataType::DateAndTime:
			return "DATE_AND_TIME";
		case DataType::TimeOfDay:
			return "TIME_OF_DAY";
		case DataType::Bool:
			return "BOOL";
		case DataType::Sint:
			return "SINT";
		case DataType::Int:
			return "INT";
		case DataType::Dint:
			return "DINT";
		case DataType::Lint:
			return "LINT";
		case DataType::Usint:
			return "USINT";
		case DataType::Uint:
			return "UINT";
		case DataType::Udint:
			return "UDINT";
		case DataType::Ulint:
			return "ULINT";
		case DataType::Real:
			return "REAL";
		case DataType::Lreal:
			return "LREAL";
		case DataType::String:
			return "STRING";
		case DataType::Byte:
			return "BYTE";
		case DataType::Word:
			return "WORD";
		case DataType::Dword:
			return "DWORD";
		case DataType::Lword:
			return "LWORD";
		case DataType::Time2:
			return "TIME2";
		case DataType::AbbreviatedStructure:
			return "ABBREVIATED_STRUCTURE";
		case DataType::Structure:
			return "STRUCTURE";
		case DataType::Array:
			return "ARRAY";
		default:
			return fmt::format("Unknown({:x})", static_cast<uint8_t>(type));
	}
}

bool is_valid_value(DataType data_type)
{
	switch (data_type)
	{
		case DataType::Date:
		case DataType::Time:
		case DataType::DateAndTime:
		case DataType::TimeOfDay:
		case DataType::Bool:
		case DataType::Sint:
		case DataType::Int:
		case DataType::Dint:
		case DataType::Lint:
		case DataType::Usint:
		case DataType::Uint:
		case DataType::Udint:
		case DataType::Ulint:
		case DataType::Real:
		case DataType::Lreal:
		case DataType::String:
		case DataType::Byte:
		case DataType::Word:
		case DataType::Dword:
		case DataType::Lword:
		case DataType::Time2:
		case DataType::AbbreviatedStructure:
		case DataType::Structure:
		case DataType::Array:
			return true;
		default:
			return false;
	}
}

std::string ArrayInfo::to_string() const
{
	return fmt::format(
		"ArrayInfo(element_type={}, element_size={}, dimensions={{{}}}, start_indices={{{}}})",
		daq::to_string(element_type),
		element_size,
		fmt::join(dimensions, ", "),
		fmt::join(start_indices, ", "));
}

std::string VariableInfo::to_string() const
{
	return fmt::format(
		"VariableInfo(name='{}', data_type={}, size={}, array_info={})",
		name,
		daq::to_string(data_type),
		size,
		array_info ? array_info->to_string() : "null");
}

// in bytes
size_t get_array_size(const std::vector<size_t> &dimensions, DataType element_type, size_t element_size)
{
	size_t dim_product = 1;
	for (const auto dim : dimensions)
	{
		dim_product *= dim;
	}

	if (element_type == DataType::Bool)
	{
		// Boolean arrays are packed into bits of full words
		const auto remainder = dim_product % 16;
		const auto full_bytes = dim_product / 8;
		if (remainder >= 8)
		{
			return full_bytes + 1;
		}
		if (remainder > 0)
		{
			return full_bytes + 2;
		}
		return full_bytes;
	}

	return dim_product * element_size;
}

std::vector<uint8_t> variable_request_path(const std::string &name)
{
	assert(name.size() < 256);
	const auto padded_length = name.size() + (name.size() % 2);
	std::vector<uint8_t> buf(2 + padded_length);
	ser::FixedBufferSerializer<std::endian::little> s(buf);
	ser::serialize(s, "\x91");
	ser::serialize(s, static_cast<uint8_t>(name.size()));
	ser::serialize(s, name);
	if (name.size() % 2 != 0)
	{
		ser::serialize(s, "\x00");
	}
	assert(!s.has_error());
	return buf;
}

void encode_get_attribute_all(ser::Serializer auto &ser, const std::string &variable_name)
{
	encode_get_attribute_all(ser, variable_request_path(variable_name));
}

VariableInfo get_variable_info(RequestContext &rc, std::string name)
{
	encode_get_attribute_all(rc.serializer, name);
	rc.request();

	VariableInfo var{.name = name};
	var.size = ser::read<uint32_t>(rc.deserializer);
	var.data_type = static_cast<DataType>(ser::read<uint8_t>(rc.deserializer));
	if (!is_valid_value(var.data_type))
	{
		logger->warn("Variable '{}' has unknown type {:#x}", name, fmt::underlying(var.data_type));
	}

	if (var.data_type == DataType::Array)
	{
		ArrayInfo arr;
		arr.element_type = static_cast<DataType>(ser::read<uint8_t>(rc.deserializer));
		if (!is_valid_value(arr.element_type))
		{
			logger->warn("Variable '{}' is array of unknown type {:#x}", name, fmt::underlying(arr.element_type));
		}
		// For arrays size is actually element size. We need to calculate the real size later (when we know more)
		arr.element_size = var.size;
		const auto num_dimensions = ser::read<uint8_t>(rc.deserializer);
		rc.deserializer.advance(1); // 1 byte padding

		for (uint8_t i = 0; i < num_dimensions; ++i)
		{
			arr.dimensions.push_back(ser::read<uint32_t>(rc.deserializer));
		}

		rc.deserializer.advance(8); // Not sure what's here
		/*const auto bit_number =*/ser::read<uint8_t>(rc.deserializer);
		rc.deserializer.advance(3); // Maybe padding?
		/*const auto variable_type_instance_id=*/ser::read<uint32_t>(rc.deserializer);

		for (uint8_t i = 0; i < num_dimensions; ++i)
		{
			arr.start_indices.push_back(ser::read<uint32_t>(rc.deserializer));
		}
		var.array_info = arr;

		var.size = get_array_size(arr.dimensions, arr.element_type, arr.element_size);
	}

	// for struct and abbreviated struct response_data[8:12] is instance_id
	if (rc.deserializer.has_error())
	{
		throw std::runtime_error("Could not decode get attribute all response for instance=0");
	}

	return var;
}

std::string CipResponse::to_string() const
{
	return fmt::format(
		"CipResponse(reply_service={:x}, general_status={:x}, extended_status({})='{}')",
		reply_service,
		general_status,
		extended_status.size(),
		to_hex(extended_status));
}

// https://rockwellautomation.custhelp.com/ci/okcsFattach/get/114390_5
std::string_view general_status_message(uint8_t status)
{
	switch (status)
	{
		case 0x00:
			return "Success";
		case 0x01:
			return "Connection Failure";
		case 0x02:
			return "Resource Unavailable";
		case 0x03:
			return "Invalid Parameter Value";
		case 0x04:
			return "Path Segment Error";
		case 0x05:
			return "Path Destination Error";
		case 0x07:
			return "Connection Lost";
		case 0x09:
			return "Invalid Attribute Value";
		case 0x0C:
			return "Object State Conflict";
		case 0x11:
			return "Reply Data Too Large";
		case 0x13:
			return "Not Enough Data";
		case 0x15:
			return "Too Much Data";
		case 0x1F:
			return "Vendor Specific Error";
		case 0x20:
			return "Invalid Parameter";
		default:
			return "";
	}
}

// https://www-osdes-com.translate.goog/oms/appSample/cipExplicitMsg.html?_x_tr_sl=auto&_x_tr_tl=en&_x_tr_hl=de&_x_tr_pto=wapp
// Some of these sound weird, because I simply copy&pasted from the google translated description (originally japanese)
std::string_view extended_status_message(std::span<const uint8_t> ext_status)
{
	if (ext_status.size() != 2)
	{
		return "";
	}
	uint16_t status = 0;
	std::memcpy(&status, ext_status.data(), sizeof(status));
	switch (status)
	{
		// general status: Object State Conflict
		case 0x8010:
			return "Downloading, starting up";
		case 0x8011:
			return "Tag memory error";

		// general status: Vendor Specific Error
		case 0x0102:
			return "The read target is a variable I/O that cannot be read.";
		case 0x2104:
			return "The read target is a variable I/O that cannot be read.";
		case 0x0104:
			return "An address or size that exceeds the segment area is specified.";
		case 0x1103:
			return "An address or size that exceeds the segment area is specified.";
		case 0x8001:
			return "Internal Abnormality";
		case 0x8007:
			return "An inaccessible variable was specified";
		case 0x8029:
			return "An area that cannot be accessed in bulk was specified in SimpleDataSegment.";
		case 0x8031:
			return "Internal error (memory allocation error)";

		// general status: Invalid Parameter
		case 0x8009:
			return "Segment Type Abnormal";
		case 0x800F:
			return "Data length information in the request data is inconsistent";
		case 0x8017:
			return "Requesting more than one element for a single data item";
		case 0x8018:
			return "Requesting 0 elements or exceeding the range of array data";
		case 0x8021:
			return "A value other than 0 or 2 was specified in the AddInfo area.";
		case 0x8022:
			return "The Data Type of the Request Service Data does not match the type of TAG information. The AddInfo Length "
						 "of the Request Service Data is not 0.";
		case 0x8023:
			return "Internal error (invalid command format)";
		case 0x8024:
			return "Internal error (invalid command length)";
		case 0x8025:
			return "Internal error (invalid parameter)";
		case 0x8027:
			return "Internal error (parameter error)";
		case 0x8028:
			return "A value outside the range was written to a variable with a subrange specified. An undefined value was "
						 "written to an Enum type variable.";

		default:
			return "";
	}
}

RequestContext::RequestContext(const plc_tag::Attributes &base_attributes)
	: tag(
			{
				.gateway = base_attributes.gateway,
				.path = base_attributes.path,
				.plc = base_attributes.plc,
				.name = "@raw",
			},
			5000)
	, serializer(send_buffer)
	, deserializer(recv_buffer)
{
	tag.create();
}

namespace
{
template <std::integral T>
T read_int(std::span<const uint8_t> data)
{
	T v = 0;
	assert(data.size() >= sizeof(v));
	std::memcpy(&v, data.data(), sizeof(v));
	return v;
}

std::optional<uint64_t> extended_status_to_int(std::span<const uint8_t> data)
{
	if (data.size() == 1)
	{
		return read_int<uint8_t>(data);
	}
	if (data.size() == 2)
	{
		return read_int<uint16_t>(data);
	}
	if (data.size() == 4)
	{
		return read_int<uint32_t>(data);
	}
	if (data.size() == 8)
	{
		return read_int<uint64_t>(data);
	}
	return std::nullopt;
}
}

CipResponse RequestContext::request()
{
	tag.send(serializer.serialized_buffer());
	const auto size = tag.get_data(recv_buffer);
	if (size > recv_buffer.size())
	{
		throw std::runtime_error(fmt::format("Receive buffer too small. {} bytes needed", size));
	}
	const auto response_data = std::span<const uint8_t>(recv_buffer.data(), size);

	deserializer = ser::FixedBufferDeserializer<std::endian::little>(response_data);
	CipResponse cip_response;
	if (!cip_response.decode(deserializer))
	{
		throw std::runtime_error("Could not decode CIP response: " + to_hex(response_data));
	}
	if (cip_response.general_status != 0)
	{
		const auto gen_message = general_status_message(cip_response.general_status);
		const auto ext_message = extended_status_message(cip_response.extended_status);
		const auto ext_status = extended_status_to_int(cip_response.extended_status);

		std::string message = fmt::format("Received error status in CIP response: {:#x}", cip_response.general_status);
		if (!cip_response.extended_status.empty())
		{
			message.append(fmt::format(", extended: {:#x}", ext_status));
		}
		if (!gen_message.empty() || !ext_message.empty())
		{
			message.append(" - ");
			if (!gen_message.empty())
			{
				message.append(gen_message);
			}
			if (!ext_message.empty())
			{
				message.append(", ");
				message.append(ext_message);
			}
		}
		throw std::runtime_error(message);
	}
	return cip_response;
}

}
