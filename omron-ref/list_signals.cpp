#include "list_signals.h"

#include <log.h>

#include "omron.h"
#include "serialization.h"
#include "string_util.h"

namespace daq
{

namespace
{
std::vector<uint8_t> address_request_path(uint8_t class_id, uint16_t instance_id)
{
	std::vector<uint8_t> buf(6);
	ser::FixedBufferSerializer<std::endian::little> s(buf);
	ser::serialize_multi(s, "\x20", class_id, "\x25\x00", instance_id);
	assert(!s.has_error());
	return buf;
}

void encode_get_attribute_all(ser::Serializer auto &ser, uint16_t instance_id)
{
	daq::encode_get_attribute_all(ser, address_request_path(0x6a, instance_id));
}

size_t get_num_variables(RequestContext &rc)
{
	encode_get_attribute_all(rc.serializer, 0);
	rc.request();
	rc.deserializer.advance(2);
	const auto num = ser::read<uint16_t>(rc.deserializer);
	if (rc.deserializer.has_error())
	{
		throw std::runtime_error("Could not decode get attribute all response for instance=0");
	}
	return num;
}

// get_variable_name and get_variables are not used anymore, but I'll keep them around, because they
// are not reliant on omron specific messages and are much simpler because they use existing commands, so
// I think they might be useful in the future.
std::string get_variable_name(RequestContext &rc, uint16_t instance_id)
{
	encode_get_attribute_all(rc.serializer, instance_id);
	rc.request();
	rc.deserializer.advance(4);
	const auto name_len = ser::read<uint8_t>(rc.deserializer);
	auto name = ser::read_string(rc.deserializer, name_len);
	if (rc.deserializer.has_error())
	{
		throw std::runtime_error("Could not decode get attribute all response for instance=" + std::to_string(instance_id));
	}
	return name;
}

std::vector<VariableInfo> get_variables(RequestContext &rc)
{
	const auto num = get_num_variables(rc);
	std::vector<VariableInfo> vars;
	vars.reserve(num);
	for (size_t i = 0; i < num; ++i)
	{
		const auto instance_id = i + 1;
		auto name = get_variable_name(rc, instance_id);
		vars.push_back(get_variable_info(rc, std::move(name)));
	}

	return vars;
}

enum class TagType : uint16_t
{
	System = 1,
	User = 2,
};

void encode_omron_get_all_instances(ser::Serializer auto &ser, uint32_t next_instance_id, TagType tag_type)
{
	ser.reset();
	const auto request_path = address_request_path(0x6a, 0);
	ser::serialize(ser, "\x5F"); // Omron specific Get All Instances
	ser::serialize(ser, static_cast<uint8_t>(request_path.size() / 2)); // in words
	ser::serialize(ser, request_path);
	ser::serialize(ser, next_instance_id); // next instance id placeholder
	ser::serialize(ser, "\x20\x00\x00\x00"); // not sure
	ser::serialize(ser, static_cast<uint16_t>(tag_type)); // tag type placeholder
	if (ser.has_error())
	{
		throw std::runtime_error("Could not encode omron attribute instances request");
	}
}

struct InstanceData
{
	uint32_t id;
	std::string name;
};

InstanceData decode_instance_data(ser::Deserializer auto &deser)
{
	InstanceData data;
	data.id = ser::read<uint32_t>(deser);
	const auto instance_data_len = ser::read<uint16_t>(deser); // includes class, instance id, name
	deser.advance(2); // class? always 6B
	deser.advance(4); // instance id again
	const auto name_len = ser::read<uint8_t>(deser);
	data.name = ser::read_string(deser, name_len);
	if (instance_data_len > 2 + 4 + 1 + name_len)
	{
		const auto remaining = instance_data_len - 2 - 4 - 1 - name_len;
		deser.advance(remaining); // mostly padding I think
	}
	return data;
}

std::vector<VariableInfo> get_variables_fast(RequestContext &rc)
{
	const auto num = get_num_variables(rc);

	std::vector<std::string> names;
	names.reserve(num);

	constexpr std::array tag_types{TagType::System, TagType::User};
	for (const auto &tag_type : tag_types)
	{
		uint32_t next_instance_id = 1;
		while (true)
		{
			encode_omron_get_all_instances(rc.serializer, next_instance_id, tag_type);

			rc.request();
			const auto num_instances = ser::read<uint16_t>(rc.deserializer);
			rc.deserializer.advance(2); // unknown

			if (num_instances == 0)
			{
				break;
			}

			for (size_t i = 0; i < num_instances; ++i)
			{
				auto instance_data = decode_instance_data(rc.deserializer);
				if (rc.deserializer.has_error())
				{
					throw std::runtime_error(fmt::format("Could not decode all instance data {}", i));
				}
				names.push_back(std::move(instance_data.name));
				next_instance_id = instance_data.id + 1;
			}
		}
	}

	if (names.size() > num)
	{
		logger->warn("Read more variable names ({}) than number of variables ({})", names.size(), num);
	}

	std::vector<VariableInfo> vars;
	for (size_t i = 0; i < num; ++i)
	{
		vars.push_back(get_variable_info(rc, std::move(names[i])));
	}
	return vars;
}
}

bool include_signal_data_type_in_list(DataType data_type)
{
	if (!is_valid_value(data_type))
	{
		return false;
	}
	if (data_type == DataType::Structure)
	{
		return false;
	}
	if (data_type == DataType::AbbreviatedStructure)
	{
		return false;
	}
	return true;
}

nlohmann::json list_signals(const plc_tag::Attributes &base_attributes)
{
	RequestContext rc(base_attributes);

	const auto vars = get_variables_fast(rc);

	auto result = nlohmann::json::array();
	for (const auto &var : vars)
	{
		// Filter out data types that should not be available.
		if (!include_signal_data_type_in_list(var.data_type))
		{
			continue;
		}

		nlohmann::json symbol;
		symbol["name"] = var.name;
		symbol["type"] = to_string(var.data_type);
		if (var.array_info)
		{
			const auto &array_info = var.array_info.value();
			// Filter out data types that should not be available.
			if (!include_signal_data_type_in_list(array_info.element_type))
			{
				continue;
			}
			symbol["type"] = to_string(array_info.element_type);
			auto dimensions = nlohmann::json::array();
			assert(array_info.dimensions.size() == array_info.start_indices.size());
			for (size_t i = 0; i < array_info.dimensions.size(); ++i)
			{
				dimensions.push_back(
					nlohmann::json({array_info.start_indices[i], array_info.start_indices[i] + array_info.dimensions[i]}));
			}
			symbol["arrayDimensions"] = dimensions;
		}
		result.push_back(symbol);
	}

	return result;
}
}
