package report

import (
	"strconv"

	"github.com/prophetcro/astro-mountain/internal/model"
)

var Round = model.Round

var RoundToInt = model.RoundToInt

var RoundOpt = model.RoundOpt

var FormatG = model.FormatG

var FormatFixed = model.FormatFixed

var FormatPyBool = model.FormatPyBool

const MissingCell = "-"

func FmtInt(o model.OptFloat) string {
	if !o.Valid {
		return MissingCell
	}
	return strconv.FormatInt(int64(model.Round(o.V, 0)), 10)
}

func FmtFloat1(o model.OptFloat) string {
	if !o.Valid {
		return MissingCell
	}
	return model.FormatPyFloat(o.V)
}

func FmtG(o model.OptFloat) string {
	if !o.Valid {
		return MissingCell
	}
	return model.FormatG(o.V)
}

func FmtStr(s model.NullString) string {
	if !s.Valid || s.V == "" {
		return MissingCell
	}
	return s.V
}

func csvInt(o model.OptFloat) string {
	if !o.Valid {
		return ""
	}
	return strconv.FormatInt(int64(model.Round(o.V, 0)), 10)
}

func csvFloat1(o model.OptFloat) string {
	if !o.Valid {
		return ""
	}
	return model.FormatPyFloat(o.V)
}

func csvStr(s model.NullString) string {
	if !s.Valid {
		return ""
	}
	return s.V
}
