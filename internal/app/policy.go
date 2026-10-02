package app

import (
	"slices"
	"strings"
	"time"
)

// ISO 3166-1 identifiers are factual reference data, not a geolocation database.
const countryCodes = "AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW"

func validCountry(c string) bool {
	return len(c) == 2 && strings.Contains(" "+countryCodes+" ", " "+c+" ")
}
func resolvePolicy(p Policy) Policy {
	if p.Mode != "builtin" {
		if p.Mode == "custom" || p.Mode == "worldwide" {
			p.Provenance = "User-provided, not independently verified"
		}
		return p
	}
	if p.Builtin != "claude-web" && p.Builtin != "claude-api" {
		return Policy{Mode: "unknown"}
	}
	// Independently checked for both Claude.ai and the commercial API on 2026-10-02.
	countries := strings.Fields("AL DZ AD AO AG AR AM AU AT AZ BS BH BD BB BE BZ BJ BT BO BA BW BR BN BG BF BI CV KH CM CA CF TD CL CO KM CD CG CR CI HR CY CZ DK DJ DM DO EC EG SV GQ ER EE SZ ET FJ FI FR GA GM GE DE GH GR GD GT GN GW GY HT HN HU IS IN ID IQ IE IL IT JM JP JO KZ KE KI KW KG LA LV LB LS LR LY LI LT LU MG MW MY MV ML MT MH MR MU MX FM MD MC MN ME MA MZ NA NR NP NL NZ NI NE NG MK NO OM PK PW PS PA PG PY PE PH PL PT QA RO RW KN LC VC WS SM ST SA SN RS SC SL SG SK SI SB SO ZA KR SS ES LK SD SR SE CH TW TJ TZ TH TL TG TO TT TN TR TM TV UG UA AE GB US UY UZ VU VA VN ZM ZW")
	return Policy{Mode: "builtin", Builtin: p.Builtin, Countries: countries, Source: "https://www.anthropic.com/supported-countries", CheckedAt: "2026-10-02", Provenance: "Official source snapshot", ExcludedRegions: map[string][]string{"UA": {"Crimea", "Donetsk", "Kherson", "Luhansk", "Zaporizhzhia"}}}
}
func regionRange(c, region string, p Policy, now time.Time) (float64, float64) {
	if p.Mode == "unknown" || p.Mode == "" || !validCountry(c) {
		return 0, 40
	}
	checked, err := time.Parse("2006-01-02", p.CheckedAt)
	if err != nil || now.Before(checked) || now.Sub(checked) > 30*24*time.Hour {
		return 0, 40
	}
	if p.Mode != "worldwide" && !slices.Contains(p.Countries, c) {
		return 40, 40
	}
	exclusions := p.ExcludedRegions[c]
	if len(exclusions) > 0 {
		if strings.TrimSpace(region) == "" {
			return 0, 40
		}
		for _, excluded := range exclusions {
			if strings.Contains(strings.ToLower(region), strings.ToLower(excluded)) {
				return 40, 40
			}
		}
		// Country/subregion precision varies; never infer that an unrecognized spelling is supported.
		return 0, 40
	}
	return 0, 0
}
