#pragma once

#include <array>

#include <nlohmann/json.hpp>

#include "plc_tag.h"

namespace daq
{

nlohmann::json list_signals(const plc_tag::Attributes &base_attributes);

}
