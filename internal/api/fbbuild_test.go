package api

import (
	"encoding/binary"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/prophetcro/astro-mountain/internal/api/openmeteo"
)

type fbVar struct {
	variable openmeteo.Variable
	altitude int16
	plevel   int16
	values   []float32
}

func buildStream(t *testing.T, startEpoch int64, interval int32, utcOffset int32,
	timezone string, vars []fbVar) []byte {
	t.Helper()

	b := flatbuffers.NewBuilder(1024)

	tzOff := b.CreateString(timezone)

	varOffsets := make([]flatbuffers.UOffsetT, 0, len(vars))
	for _, v := range vars {
		openmeteo.VariableWithValuesStartValuesVector(b, len(v.values))
		for i := len(v.values) - 1; i >= 0; i-- {
			b.PrependFloat32(v.values[i])
		}
		valuesOff := b.EndVector(len(v.values))

		openmeteo.VariableWithValuesStart(b)
		openmeteo.VariableWithValuesAddVariable(b, v.variable)
		openmeteo.VariableWithValuesAddAltitude(b, v.altitude)
		openmeteo.VariableWithValuesAddPressureLevel(b, v.plevel)
		openmeteo.VariableWithValuesAddValues(b, valuesOff)
		varOffsets = append(varOffsets, openmeteo.VariableWithValuesEnd(b))
	}

	openmeteo.VariablesWithTimeStartVariablesVector(b, len(varOffsets))
	for i := len(varOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(varOffsets[i])
	}
	varsVec := b.EndVector(len(varOffsets))

	count := 0
	if len(vars) > 0 {
		count = len(vars[0].values)
	}
	openmeteo.VariablesWithTimeStart(b)
	openmeteo.VariablesWithTimeAddTime(b, startEpoch)
	openmeteo.VariablesWithTimeAddTimeEnd(b, startEpoch+int64(count)*int64(interval))
	openmeteo.VariablesWithTimeAddInterval(b, interval)
	openmeteo.VariablesWithTimeAddVariables(b, varsVec)
	hourlyOff := openmeteo.VariablesWithTimeEnd(b)

	openmeteo.WeatherApiResponseStart(b)
	openmeteo.WeatherApiResponseAddLatitude(b, 28.25)
	openmeteo.WeatherApiResponseAddLongitude(b, 119.375)
	openmeteo.WeatherApiResponseAddElevation(b, 1000)
	openmeteo.WeatherApiResponseAddUtcOffsetSeconds(b, utcOffset)
	openmeteo.WeatherApiResponseAddTimezone(b, tzOff)
	openmeteo.WeatherApiResponseAddHourly(b, hourlyOff)
	b.Finish(openmeteo.WeatherApiResponseEnd(b))

	return prefixed(b.FinishedBytes())
}

func buildStreamWithoutHourly(t *testing.T) []byte {
	t.Helper()

	b := flatbuffers.NewBuilder(256)
	tzOff := b.CreateString("Asia/Shanghai")
	openmeteo.WeatherApiResponseStart(b)
	openmeteo.WeatherApiResponseAddLatitude(b, 28.25)
	openmeteo.WeatherApiResponseAddUtcOffsetSeconds(b, 28800)
	openmeteo.WeatherApiResponseAddTimezone(b, tzOff)
	b.Finish(openmeteo.WeatherApiResponseEnd(b))
	return prefixed(b.FinishedBytes())
}

func prefixed(msg []byte) []byte {
	out := make([]byte, 4+len(msg))
	binary.LittleEndian.PutUint32(out[:4], uint32(len(msg)))
	copy(out[4:], msg)
	return out
}

func midnightEpoch(localMidnightUnix int64, utcOffset int32) int64 {
	return localMidnightUnix - int64(utcOffset)
}
