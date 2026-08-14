package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

type tomorrowFieldLabelsJSON struct{}

func (tomorrowFieldLabelsJSON) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range TomorrowCSVFields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		label, ok := TomorrowFieldLabels[f]
		if !ok {
			label = f
		}
		val, err := json.Marshal(label)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type tomorrowCapabilitiesJSON struct {
	HasCloudTopData bool   `json:"has_cloud_top_data"`
	SeaBelowUnknown bool   `json:"sea_below_unknown"`
	Note            string `json:"note"`
}

type tomorrowReasonCountJSON struct {
	Reason string `json:"reason"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
}

type tomorrowRowJSON struct {
	Site                 string   `json:"site"`
	Night                string   `json:"night"`
	Time                 string   `json:"time"`
	TimeUTC              string   `json:"time_utc"`
	Rating               string   `json:"rating"`
	Relation             string   `json:"relation"`
	NoDataReason         string   `json:"no_data_reason"`
	NoDataLabel          string   `json:"no_data_label"`
	HModelMSL            *float64 `json:"h_model_msl"`
	DeltaH               *float64 `json:"delta_h"`
	CloudBaseAGLModel    *float64 `json:"cloud_base_agl_model"`
	CloudBaseAboveSite   *float64 `json:"cloud_base_above_site"`
	TerrainFidelity      string   `json:"terrain_fidelity"`
	TerrainFidelityLabel string   `json:"terrain_fidelity_label"`
	SeaBelowUnknown      bool     `json:"sea_below_unknown"`
	Note                 string   `json:"note"`
}

type tomorrowSiteJSON struct {
	Site                  string                    `json:"site"`
	Active                bool                      `json:"active"`
	QuotaExhausted        bool                      `json:"quota_exhausted"`
	NextAvailable         *string                   `json:"next_available"`
	RowCount              int                       `json:"row_count"`
	NoDataCount           int                       `json:"nodata_count"`
	NoDataByReason        []tomorrowReasonCountJSON `json:"nodata_by_reason"`
	Capabilities          tomorrowCapabilitiesJSON  `json:"capabilities"`
	ThresholdsReused      []string                  `json:"thresholds_reused"`
	ThresholdsUnavailable []string                  `json:"thresholds_unavailable"`
	Rows                  []tomorrowRowJSON         `json:"rows"`
}

type tomorrowTrackJSON struct {
	Name           string                    `json:"name"`
	Label          string                    `json:"label"`
	SingleTrack    bool                      `json:"single_track"`
	SiteCount      int                       `json:"site_count"`
	RowCount       int                       `json:"row_count"`
	NoDataCount    int                       `json:"nodata_count"`
	NoDataByReason []tomorrowReasonCountJSON `json:"nodata_by_reason"`
	QuotaExhausted bool                      `json:"quota_exhausted"`
	NextAvailable  *string                   `json:"next_available"`
	Capabilities   tomorrowCapabilitiesJSON  `json:"capabilities"`
}

type tomorrowPayload struct {
	FieldLabels tomorrowFieldLabelsJSON `json:"field_labels"`
	Meta        model.ReportMeta        `json:"meta"`
	Track       tomorrowTrackJSON       `json:"track"`
	Config      configExport            `json:"config"`
	Sites       []tomorrowSiteJSON      `json:"sites"`
}

const tomorrowCapabilityNote = "本轨没有云顶字段：云底低于机位时，" +
	"「脚下云海」与「机位在云中」不可区分，一律判无数据（AMBIGUOUS_BASE）。" +
	"云海判定请改用 Open-Meteo（A 轨）。"

func optHeightPtr(o model.OptFloat) *float64 {
	if !o.Valid {
		return nil
	}
	v := model.Round(o.V, 0)
	return &v
}

func tomorrowCapabilitiesOf(c dualtrack.TrackCapabilities) tomorrowCapabilitiesJSON {
	return tomorrowCapabilitiesJSON{
		HasCloudTopData: c.HasCloudTopData,
		SeaBelowUnknown: c.SeaBelowUnknown,
		Note:            tomorrowCapabilityNote,
	}
}

func tomorrowReasonsOf(tr *dualtrack.TrackResult) []tomorrowReasonCountJSON {
	out := make([]tomorrowReasonCountJSON, 0, len(TomorrowReasonOrder))
	for _, reason := range TomorrowReasonOrder {
		if n := tr.CountByReason(reason); n > 0 {
			out = append(out, tomorrowReasonCountJSON{
				Reason: string(reason), Label: reason.Label(), Count: n,
			})
		}
	}
	return out
}

func BuildTomorrowJSON(meta model.ReportMeta, tracks []*dualtrack.TrackResult,
	cfg config.Config) ([]byte, error) {

	sites := make([]tomorrowSiteJSON, 0, len(tracks))
	totalRows, totalNoData := 0, 0

	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		rows := make([]tomorrowRowJSON, 0, len(tr.Rows))
		for _, v := range tr.Rows {
			label := ""
			if v.NoDataReason != dualtrack.NoDataNone {
				label = v.NoDataReason.Label()
			}
			rows = append(rows, tomorrowRowJSON{
				Site:                 tr.SiteID,
				Night:                TomorrowNightID(v.TimeLocal),
				Time:                 tomorrowLocalISO(v.TimeLocal),
				TimeUTC:              tomorrowUTCISO(v.TimeUTC),
				Rating:               v.Rating,
				Relation:             v.Rel,
				NoDataReason:         string(v.NoDataReason),
				NoDataLabel:          label,
				HModelMSL:            optHeightPtr(v.HModelM),
				DeltaH:               optHeightPtr(v.DeltaH),
				CloudBaseAGLModel:    optHeightPtr(v.CloudBaseAGLM),
				CloudBaseAboveSite:   optHeightPtr(v.CloudBaseAboveSite),
				TerrainFidelity:      string(v.TerrainFidelity),
				TerrainFidelityLabel: v.TerrainFidelity.Label(),
				SeaBelowUnknown:      v.SeaBelowUnknown,
				Note:                 v.Note,
			})
		}

		var nextAvailable *string
		if tr.NextAvailable != nil && !tr.NextAvailable.IsZero() {
			s := tr.NextAvailable.Format(tomorrowStampLayout)
			nextAvailable = &s
		}

		reused := tr.ThresholdsReused
		if reused == nil {
			reused = []string{}
		}
		unavailable := tr.ThresholdsUnavailable
		if unavailable == nil {
			unavailable = []string{}
		}

		sites = append(sites, tomorrowSiteJSON{
			Site:                  tr.SiteID,
			Active:                tr.Active,
			QuotaExhausted:        tr.QuotaExhausted,
			NextAvailable:         nextAvailable,
			RowCount:              len(tr.Rows),
			NoDataCount:           tr.NoDataCount(),
			NoDataByReason:        tomorrowReasonsOf(tr),
			Capabilities:          tomorrowCapabilitiesOf(tr.Capabilities),
			ThresholdsReused:      reused,
			ThresholdsUnavailable: unavailable,
			Rows:                  rows,
		})
		totalRows += len(tr.Rows)
		totalNoData += tr.NoDataCount()
	}

	trackReasons := make([]tomorrowReasonCountJSON, 0, len(TomorrowReasonOrder))
	for _, rc := range TomorrowReasonCounts(tracks) {
		trackReasons = append(trackReasons, tomorrowReasonCountJSON{
			Reason: string(rc.Reason), Label: rc.Label, Count: rc.Count,
		})
	}

	var trackNext *string
	if n := TomorrowNextAvailable(tracks); n != nil {
		s := n.Format(tomorrowStampLayout)
		trackNext = &s
	}

	caps := tomorrowCapabilitiesJSON{
		HasCloudTopData: false,
		SeaBelowUnknown: true,
		Note:            tomorrowCapabilityNote,
	}
	for _, tr := range tracks {
		if tr != nil {
			caps = tomorrowCapabilitiesOf(tr.Capabilities)
			break
		}
	}

	payload := tomorrowPayload{
		Meta: meta,
		Track: tomorrowTrackJSON{
			Name:           string(tomorrowSourceName),
			Label:          TomorrowTrackLabel,
			SingleTrack:    true,
			SiteCount:      len(sites),
			RowCount:       totalRows,
			NoDataCount:    totalNoData,
			NoDataByReason: trackReasons,
			QuotaExhausted: TomorrowQuotaExhausted(tracks),
			NextAvailable:  trackNext,
			Capabilities:   caps,
		},
		Config: newConfigExport(cfg),
		Sites:  sites,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return nil, fmt.Errorf("序列化 JSON 失败：%w", err)
	}
	return buf.Bytes(), nil
}

const tomorrowSourceName = "tomorrow"

func ExportTomorrowJSON(path string, meta model.ReportMeta,
	tracks []*dualtrack.TrackResult, cfg config.Config) error {

	data, err := BuildTomorrowJSON(meta, tracks, cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入 JSON 文件 %s 失败：%w", path, err)
	}
	return nil
}
