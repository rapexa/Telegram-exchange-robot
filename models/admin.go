package models

import (
	"time"

	"gorm.io/gorm"
)

// AdminPermission represents different permission levels
type AdminPermission string

const (
	// Core permissions
	PERM_SET_USDT_RATE     AdminPermission = "set_usdt_rate"     // تعیین قیمت تتر
	PERM_SET_TRADE_PERCENT AdminPermission = "set_trade_percent" // تعیین درصد سود
	PERM_MODIFY_BALANCE    AdminPermission = "modify_balance"    // تغییر دارایی کاربر
	PERM_VIEW_WALLET       AdminPermission = "view_wallet"       // دیدن ادرس ولت و کلمات بک اپ
	PERM_VIEW_BALANCE      AdminPermission = "view_balance"      // دیدن دارایی کاربر

	// Additional admin permissions
	PERM_MANAGE_ADMINS      AdminPermission = "manage_admins"      // مدیریت ادمین‌ها (فقط سوپر ادمین)
	PERM_BROADCAST          AdminPermission = "broadcast"          // پیام همگانی
	PERM_MANAGE_WITHDRAWALS AdminPermission = "manage_withdrawals" // مدیریت برداشت‌ها
	PERM_VIEW_STATS         AdminPermission = "view_stats"         // مشاهده آمار
	PERM_SEARCH_USERS       AdminPermission = "search_users"       // جستجوی کاربران
	PERM_BACKUP_DB          AdminPermission = "backup_db"          // بکاپ دیتابیس
	PERM_SET_LIMITS         AdminPermission = "set_limits"         // تنظیم محدودیت‌ها (حداقل/حداکثر واریز و برداشت)
)

// Admin represents an admin user with specific permissions
type Admin struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TelegramID   int64     `gorm:"uniqueIndex;not null" json:"telegram_id"`
	Username     string    `gorm:"size:100" json:"username"`
	FullName     string    `gorm:"size:200" json:"full_name"`
	IsSuperAdmin bool      `gorm:"default:false" json:"is_super_admin"` // سوپر ادمین (دسترسی کامل)
	IsActive     bool      `gorm:"default:true" json:"is_active"`
	CreatedBy    *uint     `json:"created_by"` // کدام ادمین این ادمین رو ساخته
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Relations
	Permissions    []AdminPermissionRecord `gorm:"foreignKey:AdminID;constraint:OnDelete:CASCADE" json:"permissions"`
	CreatedByAdmin *Admin                  `gorm:"foreignKey:CreatedBy" json:"created_by_admin,omitempty"`
}

// AdminPermissionRecord stores individual permissions for each admin
type AdminPermissionRecord struct {
	ID         uint            `gorm:"primaryKey" json:"id"`
	AdminID    uint            `gorm:"not null;index" json:"admin_id"`
	Permission AdminPermission `gorm:"size:50;not null" json:"permission"`
	CreatedAt  time.Time       `json:"created_at"`

	// Relations
	Admin Admin `gorm:"foreignKey:AdminID" json:"admin,omitempty"`
}

// HasPermission checks if admin has a specific permission
func (a *Admin) HasPermission(db *gorm.DB, permission AdminPermission) bool {
	// Super admin has all permissions
	if a.IsSuperAdmin {
		return true
	}

	// Check if admin is active
	if !a.IsActive {
		return false
	}

	// Check specific permission
	var count int64
	db.Model(&AdminPermissionRecord{}).Where("admin_id = ? AND permission = ?", a.ID, permission).Count(&count)
	return count > 0
}

// GetPermissions returns all permissions for this admin
func (a *Admin) GetPermissions(db *gorm.DB) ([]AdminPermission, error) {
	if a.IsSuperAdmin {
		// Super admin has all permissions
		return []AdminPermission{
			PERM_SET_USDT_RATE,
			PERM_SET_TRADE_PERCENT,
			PERM_MODIFY_BALANCE,
			PERM_VIEW_WALLET,
			PERM_VIEW_BALANCE,
			PERM_MANAGE_ADMINS,
			PERM_BROADCAST,
			PERM_MANAGE_WITHDRAWALS,
			PERM_VIEW_STATS,
			PERM_SEARCH_USERS,
			PERM_BACKUP_DB,
			PERM_SET_LIMITS,
		}, nil
	}

	var records []AdminPermissionRecord
	if err := db.Where("admin_id = ?", a.ID).Find(&records).Error; err != nil {
		return nil, err
	}

	permissions := make([]AdminPermission, len(records))
	for i, record := range records {
		permissions[i] = record.Permission
	}

	return permissions, nil
}

// AddPermission adds a permission to admin
func (a *Admin) AddPermission(db *gorm.DB, permission AdminPermission) error {
	// Check if permission already exists
	var count int64
	db.Model(&AdminPermissionRecord{}).Where("admin_id = ? AND permission = ?", a.ID, permission).Count(&count)
	if count > 0 {
		return nil // Permission already exists
	}

	record := AdminPermissionRecord{
		AdminID:    a.ID,
		Permission: permission,
		CreatedAt:  time.Now(),
	}

	return db.Create(&record).Error
}

// RemovePermission removes a permission from admin
func (a *Admin) RemovePermission(db *gorm.DB, permission AdminPermission) error {
	return db.Where("admin_id = ? AND permission = ?", a.ID, permission).Delete(&AdminPermissionRecord{}).Error
}

// SetPermissions sets all permissions for admin (replaces existing ones)
func (a *Admin) SetPermissions(db *gorm.DB, permissions []AdminPermission) error {
	// Start transaction
	tx := db.Begin()

	// Remove all existing permissions
	if err := tx.Where("admin_id = ?", a.ID).Delete(&AdminPermissionRecord{}).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Add new permissions
	for _, perm := range permissions {
		record := AdminPermissionRecord{
			AdminID:    a.ID,
			Permission: perm,
			CreatedAt:  time.Now(),
		}
		if err := tx.Create(&record).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit().Error
}

// Helper functions

// GetAdminByTelegramID finds admin by telegram ID
func GetAdminByTelegramID(db *gorm.DB, telegramID int64) (*Admin, error) {
	var admin Admin
	err := db.Where("telegram_id = ? AND is_active = ?", telegramID, true).First(&admin).Error
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

// IsAdminExists checks if admin exists by telegram ID
func IsAdminExists(db *gorm.DB, telegramID int64) bool {
	var count int64
	db.Model(&Admin{}).Where("telegram_id = ? AND is_active = ?", telegramID, true).Count(&count)
	return count > 0
}

// GetAllAdmins returns all active admins
func GetAllAdmins(db *gorm.DB) ([]Admin, error) {
	var admins []Admin
	err := db.Where("is_active = ?", true).Preload("Permissions").Find(&admins).Error
	return admins, err
}

// CreateSuperAdmin creates the first super admin
func CreateSuperAdmin(db *gorm.DB, telegramID int64, username, fullName string) (*Admin, error) {
	admin := Admin{
		TelegramID:   telegramID,
		Username:     username,
		FullName:     fullName,
		IsSuperAdmin: true,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err := db.Create(&admin).Error
	if err != nil {
		return nil, err
	}

	return &admin, nil
}

// Permission descriptions in Persian
func GetPermissionDescription(permission AdminPermission) string {
	descriptions := map[AdminPermission]string{
		PERM_SET_USDT_RATE:      "تعیین قیمت تتر",
		PERM_SET_TRADE_PERCENT:  "تعیین درصد سود",
		PERM_MODIFY_BALANCE:     "تغییر دارایی کاربر",
		PERM_VIEW_WALLET:        "دیدن ادرس ولت و کلمات بک اپ",
		PERM_VIEW_BALANCE:       "دیدن دارایی کاربر",
		PERM_MANAGE_ADMINS:      "مدیریت ادمین‌ها",
		PERM_BROADCAST:          "پیام همگانی",
		PERM_MANAGE_WITHDRAWALS: "مدیریت برداشت‌ها",
		PERM_VIEW_STATS:         "مشاهده آمار",
		PERM_SEARCH_USERS:       "جستجوی کاربران",
		PERM_BACKUP_DB:          "بکاپ دیتابیس",
		PERM_SET_LIMITS:         "تنظیم محدودیت‌ها",
	}

	if desc, exists := descriptions[permission]; exists {
		return desc
	}
	return string(permission)
}

// GetAllPermissions returns all available permissions
func GetAllPermissions() []AdminPermission {
	return []AdminPermission{
		PERM_SET_USDT_RATE,
		PERM_SET_TRADE_PERCENT,
		PERM_MODIFY_BALANCE,
		PERM_VIEW_WALLET,
		PERM_VIEW_BALANCE,
		PERM_MANAGE_ADMINS,
		PERM_BROADCAST,
		PERM_MANAGE_WITHDRAWALS,
		PERM_VIEW_STATS,
		PERM_SEARCH_USERS,
		PERM_BACKUP_DB,
		PERM_SET_LIMITS,
	}
}

// AdminLog represents an admin action log entry
type AdminLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	AdminID    uint      `gorm:"not null;index" json:"admin_id"`
	AdminTgID  int64     `gorm:"not null;index" json:"admin_telegram_id"` // برای جستجوی سریع
	Action     string    `gorm:"size:100;not null" json:"action"`         // "add_usdt", "set_rate", "approve_withdraw", etc.
	TargetType string    `gorm:"size:50" json:"target_type"`              // "user", "admin", "transaction", "setting"
	TargetID   *uint     `json:"target_id"`                               // ID هدف (مثلاً User ID یا Transaction ID)
	Details    string    `gorm:"type:text" json:"details"`                // جزئیات بیشتر (JSON یا متن)
	IPAddress  string    `gorm:"size:45" json:"ip_address"`               // IP address (اختیاری)
	CreatedAt  time.Time `json:"created_at"`

	// Relations
	Admin Admin `gorm:"foreignKey:AdminID" json:"admin,omitempty"`
}

// LogAdminAction logs an admin action to the database
func LogAdminAction(db *gorm.DB, adminID uint, adminTgID int64, action, targetType string, targetID *uint, details string) error {
	logEntry := AdminLog{
		AdminID:    adminID,
		AdminTgID:  adminTgID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Details:    details,
		CreatedAt:  time.Now(),
	}
	return db.Create(&logEntry).Error
}

// GetAdminLogs retrieves admin logs with optional filters
func GetAdminLogs(db *gorm.DB, adminID *uint, limit int) ([]AdminLog, error) {
	var logs []AdminLog
	query := db.Model(&AdminLog{}).Order("created_at DESC")

	if adminID != nil {
		query = query.Where("admin_id = ?", *adminID)
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&logs).Error
	return logs, err
}
