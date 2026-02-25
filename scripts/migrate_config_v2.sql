-- ============================================
-- 配置项重构 v2 迁移脚本
-- 将 thumbnail, conversion, thumbnail_scanner 合并为 image_processing
-- 删除 cache, upload, server, rate_limit 配置（已移至环境变量）
-- ============================================

-- 开始事务
BEGIN;

-- ============================================
-- 步骤1: 检查是否存在需要迁移的配置
-- ============================================

-- 检查需要迁移的配置分类
SELECT 'Found categories to migrate:' as info;
SELECT DISTINCT category FROM system_configs WHERE category IN ('thumbnail', 'conversion', 'thumbnail_scanner', 'cache', 'upload', 'server', 'rate_limit');

-- ============================================
-- 步骤2: 创建新的 image_processing 配置
-- ============================================

-- 从 thumbnail 配置迁移数据
INSERT INTO system_configs (
    category,
    name,
    key,
    config_json,
    is_enabled,
    is_default,
    priority,
    description,
    created_by,
    created_at,
    updated_at
)
SELECT
    'image_processing' as category,
    'Image Processing Settings' as name,
    'image_processing:default' as key,
    config_json,
    is_enabled,
    is_default,
    priority,
    'Migrated from thumbnail config' as description,
    created_by,
    created_at,
    updated_at
FROM system_configs
WHERE category = 'thumbnail'
AND NOT EXISTS (
    SELECT 1 FROM system_configs WHERE category = 'image_processing'
);

-- ============================================
-- 步骤3: 删除旧配置分类
-- ============================================

-- 删除已合并的配置分类
DELETE FROM system_configs WHERE category IN ('thumbnail', 'conversion', 'thumbnail_scanner');

-- 删除已移至环境变量的配置分类
DELETE FROM system_configs WHERE category IN ('cache', 'upload', 'server', 'rate_limit');

-- ============================================
-- 步骤4: 验证迁移结果
-- ============================================

-- 显示迁移后的配置分类
SELECT 'Remaining categories after migration:' as info;
SELECT DISTINCT category FROM system_configs ORDER BY category;

-- 显示 image_processing 配置
SELECT 'Image processing config count:' as info, COUNT(*) as count FROM system_configs WHERE category = 'image_processing';

-- 提交事务
COMMIT;

-- ============================================
-- 迁移完成
-- ============================================
-- 注意事项:
-- 1. cache, upload, server, rate_limit 配置现在从环境变量读取
-- 2. 请确保设置了相应的环境变量
-- 3. thumbnail, conversion, scanner 配置已合并到 image_processing
-- ============================================
