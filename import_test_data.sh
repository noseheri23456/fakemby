#!/bin/bash

# FakEmby 测试数据导入脚本
# 导入 Big Buck Bunny 和其他测试媒体

set -e

BASE_URL="http://localhost:8096"
ADMIN_API_KEY="admin-key"

echo "========================================"
echo "FakEmby 测试数据导入"
echo "========================================"
echo ""

# 1. 创建电影库
echo "[1] 创建电影库..."
LIB_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "电影库",
    "Type": "Folder",
    "CollectionType": "movies"
  }')

LIB_ID=$(echo $LIB_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "✓ 电影库 ID: $LIB_ID"
echo ""

# 2. 导入 Big Buck Bunny
echo "[2] 导入 Big Buck Bunny..."
ITEM_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Big Buck Bunny",
    "Type": "Movie",
    "ParentId": "'$LIB_ID'",
    "Overview": "Big Buck Bunny is a short animated comedy film about a large rabbit dealing with three tiny creatures building a dam on his land.",
    "Year": 2008,
    "PremiereDate": "2008-04-10",
    "CommunityRating": 7.8,
    "OfficialRating": "PG",
    "RuntimeTicks": 600000000000,
    "Genres": ["Animation", "Comedy", "Short"],
    "Tags": ["Open Source", "Blender", "Test"],
    "Studios": ["Blender Foundation"]
  }')

ITEM_ID=$(echo $ITEM_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "✓ 项目 ID: $ITEM_ID"
echo ""

# 3. 添加播放源 (Big Buck Bunny 1080p)
echo "[3] 添加播放源 (1080p)..."
curl -s -X POST "$BASE_URL/api/admin/items/$ITEM_ID/sources" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "1080p (MP4)",
    "URL": "https://peach.blender.org/download/bbb_sunflower_1080p_h264.mov",
    "Container": "mov",
    "Bitrate": 5000000
  }' > /dev/null
echo "✓ 1080p 源添加"

# 4. 添加播放源 (480p 版本)
echo "[4] 添加播放源 (480p)..."
curl -s -X POST "$BASE_URL/api/admin/items/$ITEM_ID/sources" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "480p (MP4)",
    "URL": "https://peach.blender.org/download/bbb_sunflower_480p_h264.mov",
    "Container": "mov",
    "Bitrate": 1000000
  }' > /dev/null
echo "✓ 480p 源添加"
echo ""

# 5. 添加海报
echo "[5] 添加海报..."
curl -s -X POST "$BASE_URL/api/admin/items/$ITEM_ID/images" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Type": "Primary",
    "URL": "https://peach.blender.org/download/bbb_poster.jpg"
  }' > /dev/null 2>&1 || echo "⚠ 海报添加可能失败"
echo "✓ 海报添加"
echo ""

# 6. 创建电视剧库
echo "[6] 创建电视剧库..."
TV_LIB_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "电视剧库",
    "Type": "Folder",
    "CollectionType": "tvshows"
  }')

TV_LIB_ID=$(echo $TV_LIB_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "✓ 电视剧库 ID: $TV_LIB_ID"
echo ""

# 7. 创建示例电视剧
echo "[7] 创建示例电视剧 (Test Series)..."
SERIES_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Test Series",
    "Type": "Series",
    "ParentId": "'$TV_LIB_ID'",
    "Overview": "这是一个测试电视剧系列",
    "Year": 2024,
    "Genres": ["Test", "Drama"]
  }')

SERIES_ID=$(echo $SERIES_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "✓ 电视剧 ID: $SERIES_ID"
echo ""

# 8. 创建第一季
echo "[8] 创建第一季..."
SEASON_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Season 1",
    "Type": "Season",
    "ParentId": "'$SERIES_ID'",
    "SeasonNumber": 1
  }')

SEASON_ID=$(echo $SEASON_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "✓ 第一季 ID: $SEASON_ID"
echo ""

# 9. 创建第一集
echo "[9] 创建第一集..."
EP_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Episode 1",
    "Type": "Episode",
    "ParentId": "'$SEASON_ID'",
    "SeasonNumber": 1,
    "EpisodeNumber": 1,
    "Overview": "测试剧集 1",
    "RuntimeTicks": 300000000000
  }')

EP_ID=$(echo $EP_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "✓ 第一集 ID: $EP_ID"
echo ""

# 10. 为第一集添加播放源
echo "[10] 为第一集添加播放源..."
curl -s -X POST "$BASE_URL/api/admin/items/$EP_ID/sources" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Test Episode Stream",
    "URL": "https://peach.blender.org/download/bbb_sunflower_1080p_h264.mov",
    "Container": "mov"
  }' > /dev/null
echo "✓ 播放源添加"
echo ""

# 11. 创建第二集
echo "[11] 创建第二集..."
EP2_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Episode 2",
    "Type": "Episode",
    "ParentId": "'$SEASON_ID'",
    "SeasonNumber": 1,
    "EpisodeNumber": 2,
    "Overview": "测试剧集 2",
    "RuntimeTicks": 300000000000
  }')

EP2_ID=$(echo $EP2_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "✓ 第二集 ID: $EP2_ID"
echo ""

echo "========================================"
echo "✓ 测试数据导入完成！"
echo "========================================"
echo ""
echo "创建的项目："
echo "  - 电影库: $LIB_ID"
echo "    └─ Big Buck Bunny: $ITEM_ID"
echo ""
echo "  - 电视剧库: $TV_LIB_ID"
echo "    └─ Test Series: $SERIES_ID"
echo "      └─ Season 1: $SEASON_ID"
echo "        ├─ Episode 1: $EP_ID"
echo "        └─ Episode 2: $EP2_ID"
echo ""
echo "现在可以使用以下 URL 访问服务器："
echo "  - 浏览器: http://localhost:8096"
echo "  - 小幻影视: 添加服务器 http://localhost:8096"
echo "  - SenPlayer: 添加服务器 http://localhost:8096"
echo ""
echo "默认登录："
echo "  用户名: admin"
echo "  密码: admin"
echo ""
