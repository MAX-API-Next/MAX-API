package task_billing_setting

func defaultRateCards() map[string]RateCard {
	v3Rows := []RateCardRow{
		row("std_no_audio", 0.6, "quality", "std", "has_audio", "false"),
		row("std_audio", 0.9, "quality", "std", "has_audio", "true"),
		row("pro_no_audio", 0.8, "quality", "pro", "has_audio", "false"),
		row("pro_audio", 1.2, "quality", "pro", "has_audio", "true"),
		row("4k_no_audio", 3.0, "quality", "4k", "has_audio", "false"),
		row("4k_audio", 3.0, "quality", "4k", "has_audio", "true"),
	}
	v3OmniRows := []RateCardRow{
		row("std_no_video_no_audio", 0.6, "quality", "std", "has_video_input", "false", "has_audio", "false"),
		row("std_no_video_audio", 0.8, "quality", "std", "has_video_input", "false", "has_audio", "true"),
		row("std_video_no_audio", 0.9, "quality", "std", "has_video_input", "true", "has_audio", "false"),
		row("pro_no_video_no_audio", 0.8, "quality", "pro", "has_video_input", "false", "has_audio", "false"),
		row("pro_no_video_audio", 1.0, "quality", "pro", "has_video_input", "false", "has_audio", "true"),
		row("pro_video_no_audio", 1.2, "quality", "pro", "has_video_input", "true", "has_audio", "false"),
		row("4k_no_video_no_audio", 3.0, "quality", "4k", "has_video_input", "false", "has_audio", "false"),
		row("4k_no_video_audio", 3.0, "quality", "4k", "has_video_input", "false", "has_audio", "true"),
	}
	o1Rows := []RateCardRow{
		row("std_video", 0.9, "quality", "std", "has_video_input", "true"),
		row("std_no_video", 0.6, "quality", "std", "has_video_input", "false"),
		row("pro_video", 1.2, "quality", "pro", "has_video_input", "true"),
		row("pro_no_video", 0.8, "quality", "pro", "has_video_input", "false"),
	}
	return map[string]RateCard{
		"kling/kling-v3-video-generation":      perSecondCard("kling", v3Rows),
		"kling-v3":                             perSecondCard("kling", v3Rows),
		"kling/kling-v3-omni-video-generation": perSecondCard("kling", v3OmniRows),
		"kling-v3-omni":                        perSecondCard("kling", v3OmniRows),
		"kling-video-o1":                       perSecondCard("kling", o1Rows),
	}
}

func perSecondCard(vendor string, rows []RateCardRow) RateCard {
	return RateCard{
		Vendor:          vendor,
		Unit:            "second",
		QuantityField:   "duration",
		DefaultQuantity: 5,
		Strict:          true,
		Defaults: map[string]string{
			"quality":         "std",
			"has_video_input": "false",
			"has_audio":       "false",
		},
		Rows: rows,
	}
}

func row(id string, unitPrice float64, kv ...string) RateCardRow {
	match := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		match[kv[i]] = kv[i+1]
	}
	return RateCardRow{
		ID:        id,
		Match:     match,
		UnitPrice: unitPrice,
	}
}
