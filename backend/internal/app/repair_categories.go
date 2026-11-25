package app

import "strings"

type repairCategoryDefinition struct {
	code, label string
	terms       []string
}

var repairCategories = []repairCategoryDefinition{
	{"lighting", "照明", []string{"照明", "灯"}},
	{"electrical", "电器", []string{"电器", "插座"}},
	{"plumbing", "给排水", []string{"给排水", "水管", "水龙头", "漏水", "下水道"}},
	{"hvac", "空调与通风", []string{"空调", "通风", "排风", "不制冷"}},
	{"doors_windows", "门窗与锁具", []string{"门窗", "门锁", "窗户", "门把手", "锁具"}},
	{"furniture", "家具", []string{"家具", "桌椅", "课桌", "床架", "柜门"}},
	{"network", "校园网络", []string{"校园网", "网络故障", "网络报修", "断网", "wifi", "网口"}},
	{"building", "房屋设施", []string{"房屋设施", "墙面", "地面", "天花板", "吊顶"}},
	{"other", "其他", []string{"其他类", "其他故障", "其他报修"}},
}

var businessCategoryNames = func() map[string]string {
	names := make(map[string]string, len(repairCategories))
	for _, category := range repairCategories {
		names[category.code] = category.label
	}
	return names
}()

func categoryOK(code string) bool { return businessCategoryNames[code] != "" }

func categoryCodes() []string {
	codes := make([]string, 0, len(repairCategories))
	for _, category := range repairCategories {
		codes = append(codes, category.code)
	}
	return codes
}

func categoryLabels() string {
	labels := make([]string, 0, len(repairCategories))
	for _, category := range repairCategories {
		labels = append(labels, category.label)
	}
	return strings.Join(labels, "、")
}

func categoryPrompt() string {
	labels := make([]string, 0, len(repairCategories))
	for _, category := range repairCategories {
		labels = append(labels, category.code+"（"+category.label+"）")
	}
	return strings.Join(labels, "、")
}

// Prefer explicit category names and specific equipment over broad symptoms
// such as a leak or an indicator light.
func repairCategory(question string) string {
	question = strings.ToLower(strings.TrimSpace(question))
	if question == "其他" || question == "其他的" {
		return "other"
	}
	for _, category := range repairCategories {
		if category.code != "other" && strings.Contains(question, category.label) {
			return category.code
		}
	}
	for _, category := range repairCategories[3:] {
		if containsAny(question, category.terms...) {
			return category.code
		}
	}
	for _, category := range repairCategories[:3] {
		if containsAny(question, category.terms...) {
			return category.code
		}
	}
	return ""
}
