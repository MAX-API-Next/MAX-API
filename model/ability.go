package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where(commonGroupCol+" = ? and enabled = ?", group, true).Distinct("model").Pluck("model", &models)
	return models
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func GetChannel(group string, model string, retry int, requestPath string) (*Channel, error) {
	channelQuery, err := getChannelQuery(group, model, retry)
	if err != nil {
		return nil, err
	}
	abilityRows, err := findAbilityRows(channelQuery)
	if err != nil {
		return nil, err
	}
	abilities, err := filterAbilityRowsByRequestPath(abilityRows, requestPath)
	if err != nil {
		return nil, err
	}
	if len(abilities) == 0 && requestPath != "" {
		abilities, err = getFilteredAbilitiesForRequestPath(group, model, requestPath)
		if err != nil {
			return nil, err
		}
	}
	if len(abilities) == 0 {
		return nil, nil
	}

	channelId, ok := selectChannelIdFromAbilities(abilities, retry)
	if !ok {
		return nil, nil
	}
	channel := Channel{}
	err = DB.First(&channel, "id = ?", channelId).Error
	return &channel, err
}

func getPriority(group string, model string, retry int) (int64, error) {
	var priorities []int64
	err := DB.Model(&Ability{}).
		Select("DISTINCT(priority)").
		Where(abilityCol("group")+" = ? and "+abilityCol("model")+" = ? and "+abilityCol("enabled")+" = ?", group, model, true).
		Order("priority DESC").
		Pluck("priority", &priorities).Error
	if err != nil {
		return 0, err
	}
	if len(priorities) == 0 {
		return 0, errors.New("数据库一致性被破坏")
	}
	if retry >= len(priorities) {
		return priorities[len(priorities)-1], nil
	}
	return priorities[retry], nil
}

func getChannelQuery(group string, model string, retry int) (*gorm.DB, error) {
	maxPrioritySubQuery := DB.Model(&Ability{}).
		Select("MAX(priority)").
		Where(abilityCol("group")+" = ? and "+abilityCol("model")+" = ? and "+abilityCol("enabled")+" = ?", group, model, true)
	channelQuery := DB.Model(&Ability{}).
		Where(abilityCol("group")+" = ? and "+abilityCol("model")+" = ? and "+abilityCol("enabled")+" = ? and "+abilityCol("priority")+" = (?)", group, model, true, maxPrioritySubQuery)
	if retry != 0 {
		priority, err := getPriority(group, model, retry)
		if err != nil {
			return nil, err
		}
		channelQuery = DB.Model(&Ability{}).
			Where(abilityCol("group")+" = ? and "+abilityCol("model")+" = ? and "+abilityCol("enabled")+" = ? and "+abilityCol("priority")+" = ?", group, model, true, priority)
	}
	return channelQuery, nil
}

func getFilteredAbilitiesForRequestPath(group string, model string, requestPath string) ([]Ability, error) {
	channelQuery := DB.Model(&Ability{}).
		Where(abilityCol("group")+" = ? and "+abilityCol("model")+" = ? and "+abilityCol("enabled")+" = ?", group, model, true)
	abilityRows, err := findAbilityRows(channelQuery)
	if err != nil {
		return nil, err
	}
	return filterAbilityRowsByRequestPath(abilityRows, requestPath)
}

func findAbilityRows(channelQuery *gorm.DB) ([]AbilityWithChannel, error) {
	var abilityRows []AbilityWithChannel
	err := channelQuery.
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Order("abilities.weight DESC").
		Scan(&abilityRows).Error
	return abilityRows, err
}

func abilityCol(name string) string {
	if common.UsingPostgreSQL {
		return `"abilities"."` + name + `"`
	}
	return "`abilities`.`" + name + "`"
}

func selectChannelIdFromAbilities(abilities []Ability, retry int) (int, bool) {
	if len(abilities) == 0 {
		return 0, false
	}
	priorities := make([]int64, 0, len(abilities))
	seenPriorities := make(map[int64]struct{}, len(abilities))
	for _, ability := range abilities {
		priority := abilityPriority(ability)
		if _, ok := seenPriorities[priority]; ok {
			continue
		}
		seenPriorities[priority] = struct{}{}
		priorities = append(priorities, priority)
	}
	sort.Slice(priorities, func(i, j int) bool {
		return priorities[i] > priorities[j]
	})
	if retry >= len(priorities) {
		retry = len(priorities) - 1
	}
	targetPriority := priorities[retry]

	targetAbilities := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		if abilityPriority(ability) == targetPriority {
			targetAbilities = append(targetAbilities, ability)
		}
	}
	if len(targetAbilities) == 0 {
		return 0, false
	}

	weightSum := uint(0)
	for _, ability_ := range targetAbilities {
		weightSum += ability_.Weight + 10
	}
	weight := common.GetRandomInt(int(weightSum))
	for _, ability_ := range targetAbilities {
		weight -= int(ability_.Weight) + 10
		if weight <= 0 {
			return ability_.ChannelId, true
		}
	}
	return 0, false
}

func abilityPriority(ability Ability) int64 {
	if ability.Priority == nil {
		return 0
	}
	return *ability.Priority
}

func filterAbilityRowsByRequestPath(abilityRows []AbilityWithChannel, requestPath string) ([]Ability, error) {
	abilities := make([]Ability, 0, len(abilityRows))
	for _, row := range abilityRows {
		abilities = append(abilities, row.Ability)
	}
	if requestPath == "" || len(abilityRows) == 0 {
		return abilities, nil
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, row := range abilityRows {
		if row.ChannelType != constant.ChannelTypeAdvancedCustom {
			continue
		}
		if _, ok := seen[row.ChannelId]; ok {
			continue
		}
		seen[row.ChannelId] = struct{}{}
		channelIds = append(channelIds, row.ChannelId)
	}
	if len(channelIds) == 0 {
		return abilities, nil
	}

	var channels []*Channel
	if err := DB.Select("id", "type", "settings").
		Where("id IN ? AND type = ?", channelIds, constant.ChannelTypeAdvancedCustom).
		Find(&channels).Error; err != nil {
		return nil, err
	}

	advancedChannelIds := make(map[int]struct{}, len(channels))
	advancedConfigs := make(map[int]*dto.AdvancedCustomConfig)
	for _, channel := range channels {
		advancedChannelIds[channel.Id] = struct{}{}
		config, err := validatedAdvancedCustomConfigFromChannel(channel)
		if err == nil && config != nil {
			advancedConfigs[channel.Id] = config
		} else if err != nil {
			common.SysError(fmt.Sprintf("failed to parse advanced custom config: channel_id=%d, error=%v", channel.Id, err))
		} else {
			common.SysError(fmt.Sprintf("failed to parse advanced custom config: channel_id=%d, error=advanced_custom is required", channel.Id))
		}
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		_, isAdvancedCustom := advancedChannelIds[ability.ChannelId]
		if !isAdvancedCustom {
			filtered = append(filtered, ability)
			continue
		}
		if config := advancedConfigs[ability.ChannelId]; config != nil && config.SupportsPath(requestPath) {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// First delete all abilities of this channel
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	return DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	return DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingSQLite {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	return successCount, failCount, nil
}
