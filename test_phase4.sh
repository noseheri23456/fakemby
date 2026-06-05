#!/bin/bash

# Phase 4 图片处理功能测试脚本

BASE_URL="http://localhost:8096"
API_KEY="test-api-key"

echo "=== Phase 4: 图片处理功能测试 ==="
echo ""

# 1. 启动服务器
echo "1. 启动服务器..."
./fakemby.exe &
PROCESS_ID=$!
sleep 2

# 2. 登录获取 Token
echo "2. 登录获取 Token..."
TOKEN=$(curl -s -X POST "$BASE_URL/emby/Users/AuthenticateByName" \
  -H "Content-Type: application/json" \
  -d '{"Username":"admin","Pw":"admin"}' | grep -o '"AccessToken":"[^"]*' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
  echo "❌ 获取 Token 失败"
  kill $PROCESS_ID
  exit 1
fi

echo "✓ Token: $TOKEN"
echo ""

# 3. 创建测试库
echo "3. 创建测试库..."
LIB_ID=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: admin-key" \
  -d '{
    "Name": "测试库",
    "Type": "Folder",
    "CollectionType": "movies"
  }' | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)

echo "✓ 库 ID: $LIB_ID"
echo ""

# 4. 创建带图片的媒体项目
echo "4. 创建带图片的媒体项目..."
ITEM_ID=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: admin-key" \
  -d '{
    "Name": "测试电影",
    "Type": "Movie",
    "ParentId": "'$LIB_ID'",
    "Overview": "测试概述",
    "Images": [
      {
        "Type": "Primary",
        "URL": "https://via.placeholder.com/300x400?text=Primary"
      },
      {
        "Type": "Backdrop",
        "URL": "https://via.placeholder.com/1280x720?text=Backdrop"
      }
    ]
  }' | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)

echo "✓ 项目 ID: $ITEM_ID"
echo ""

# 5. 测试图片重定向模式（GET /emby/Items/{ItemId}/Images/{Type}）
echo "5. 测试图片重定向模式..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/emby/Items/$ITEM_ID/Images/Primary" \
  -H "X-Emby-Token: $TOKEN")

if [ "$HTTP_CODE" = "302" ] || [ "$HTTP_CODE" = "200" ]; then
  echo "✓ 图片重定向成功 (HTTP $HTTP_CODE)"
else
  echo "❌ 图片重定向失败 (HTTP $HTTP_CODE)"
fi

echo ""

# 6. 获取项目详情验证 ImageTags
echo "6. 获取项目详情验证 ImageTags..."
DETAILS=$(curl -s "$BASE_URL/emby/Users/1/Items/$ITEM_ID" \
  -H "X-Emby-Token: $TOKEN")

PRIMARY_TAG=$(echo $DETAILS | grep -o '"Primary":"[^"]*' | cut -d'"' -f4)
BACKDROP_TAG=$(echo $DETAILS | grep -o '"Backdrop":"[^"]*' | cut -d'"' -f4)

if [ ! -z "$PRIMARY_TAG" ]; then
  echo "✓ Primary ImageTag: $PRIMARY_TAG"
else
  echo "❌ Primary ImageTag 缺失"
fi

if [ ! -z "$BACKDROP_TAG" ]; then
  echo "✓ Backdrop ImageTag: $BACKDROP_TAG"
else
  echo "❌ Backdrop ImageTag 缺失"
fi

echo ""

# 7. 测试带索引的图片请求（GET /emby/Items/{ItemId}/Images/{Type}/{Index}）
echo "7. 测试带索引的图片请求..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/emby/Items/$ITEM_ID/Images/Primary/0" \
  -H "X-Emby-Token: $TOKEN")

if [ "$HTTP_CODE" = "302" ] || [ "$HTTP_CODE" = "200" ]; then
  echo "✓ 索引图片请求成功 (HTTP $HTTP_CODE)"
else
  echo "❌ 索引图片请求失败 (HTTP $HTTP_CODE)"
fi

echo ""

# 清理
echo "8. 清理..."
kill $PROCESS_ID 2>/dev/null || true
sleep 1

echo ""
echo "=== 测试完成 ==="
