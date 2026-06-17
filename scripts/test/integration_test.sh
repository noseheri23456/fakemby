#!/bin/bash

# FakEmby 集成测试脚本
# 测试所有主要功能：认证、媒体浏览、搜索、播放、进度同�?
set -e

BASE_URL="http://localhost:8096"
ADMIN_API_KEY="admin-key"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "========================================"
echo "FakEmby 集成测试"
echo "========================================"
echo ""

# 测试计数
TESTS_PASSED=0
TESTS_FAILED=0

# 测试函数
test_endpoint() {
    local name=$1
    local method=$2
    local path=$3
    local data=$4
    local expected_code=$5
    local headers=${6:-""}

    local cmd="curl -s -X $method \"$BASE_URL$path\" -w \"\\n%{http_code}\" $headers"
    if [ ! -z "$data" ]; then
        cmd="$cmd -d '$data'"
    fi

    local response=$(eval $cmd)
    local http_code=$(echo "$response" | tail -1)
    local body=$(echo "$response" | sed '$d')

    if [ "$http_code" = "$expected_code" ]; then
        echo -e "${GREEN}�?{NC} $name (HTTP $http_code)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        echo "$body"
    else
        echo -e "${RED}�?{NC} $name (Expected $expected_code, got $http_code)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        echo "Response: $body"
    fi
    echo ""
}

# 1. 启动服务�?echo -e "${YELLOW}[1] 启动服务�?..${NC}"
if pgrep -x "fakemby.exe" > /dev/null; then
    echo "服务器已运行"
else
    ./fakemby.exe &
    FAKEMBY_PID=$!
    sleep 2
fi
echo ""

# 2. 系统信息
echo -e "${YELLOW}[2] 测试系统端点...${NC}"
test_endpoint "GET /emby/System/Info/Public" "GET" "/emby/System/Info/Public" "" "200"

# 3. 登录
echo -e "${YELLOW}[3] 测试认证...${NC}"
LOGIN_RESPONSE=$(curl -s -X POST "$BASE_URL/emby/Users/AuthenticateByName" \
    -H "Content-Type: application/json" \
    -d '{"Username":"admin","Pw":"admin"}')

TOKEN=$(echo $LOGIN_RESPONSE | grep -o '"AccessToken":"[^"]*' | cut -d'"' -f4)
USER_ID=$(echo $LOGIN_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)

if [ ! -z "$TOKEN" ] && [ ! -z "$USER_ID" ]; then
    echo -e "${GREEN}�?{NC} 成功获取 Token: $TOKEN"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo -e "${RED}�?{NC} 登录失败"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
echo ""

# 4. 获取用户信息
echo -e "${YELLOW}[4] 测试用户端点...${NC}"
test_endpoint "GET /emby/Users/Public" "GET" "/emby/Users/Public" "" "200" \
    "-H 'X-Emby-Token: $TOKEN'"

# 5. 创建媒体�?echo -e "${YELLOW}[5] 测试媒体库创�?..${NC}"
LIB_CREATE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
    -H "Content-Type: application/json" \
    -H "X-Api-Key: $ADMIN_API_KEY" \
    -d '{
        "Name": "测试电影�?,
        "Type": "Folder",
        "CollectionType": "movies"
    }')

LIB_ID=$(echo $LIB_CREATE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
if [ ! -z "$LIB_ID" ]; then
    echo -e "${GREEN}�?{NC} 创建媒体�? $LIB_ID"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo -e "${RED}�?{NC} 创建媒体库失�?
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
echo ""

# 6. 创建媒体项目
echo -e "${YELLOW}[6] 测试媒体项目创建...${NC}"
ITEM_CREATE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
    -H "Content-Type: application/json" \
    -H "X-Api-Key: $ADMIN_API_KEY" \
    -d '{
        "Name": "测试电影",
        "Type": "Movie",
        "ParentId": "'$LIB_ID'",
        "Overview": "这是一部测试电�?,
        "Year": 2024
    }')

ITEM_ID=$(echo $ITEM_CREATE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
if [ ! -z "$ITEM_ID" ]; then
    echo -e "${GREEN}�?{NC} 创建媒体项目: $ITEM_ID"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    echo -e "${RED}�?{NC} 创建媒体项目失败"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi
echo ""

# 7. 获取媒体库视�?echo -e "${YELLOW}[7] 测试媒体库视�?..${NC}"
test_endpoint "GET /emby/Users/{id}/Views" "GET" "/emby/Users/$USER_ID/Views" "" "200" \
    "-H 'X-Emby-Token: $TOKEN'"

# 8. 获取媒体项目列表
echo -e "${YELLOW}[8] 测试媒体项目列表...${NC}"
test_endpoint "GET /emby/Users/{id}/Items" "GET" "/emby/Users/$USER_ID/Items?ParentId=$LIB_ID&Limit=20" "" "200" \
    "-H 'X-Emby-Token: $TOKEN'"

# 9. 获取媒体项目详情
echo -e "${YELLOW}[9] 测试媒体项目详情...${NC}"
if [ ! -z "$ITEM_ID" ]; then
    test_endpoint "GET /emby/Users/{id}/Items/{id}" "GET" "/emby/Users/$USER_ID/Items/$ITEM_ID" "" "200" \
        "-H 'X-Emby-Token: $TOKEN'"
fi

# 10. 搜索
echo -e "${YELLOW}[10] 测试搜索功能...${NC}"
test_endpoint "GET /emby/Search/Hints" "GET" "/emby/Search/Hints?SearchTerm=测试&Limit=10" "" "200" \
    "-H 'X-Emby-Token: $TOKEN'"

# 11. 添加播放�?echo -e "${YELLOW}[11] 测试添加播放�?..${NC}"
if [ ! -z "$ITEM_ID" ]; then
    SOURCE_CREATE=$(curl -s -X POST "$BASE_URL/api/admin/items/$ITEM_ID/sources" \
        -H "Content-Type: application/json" \
        -H "X-Api-Key: $ADMIN_API_KEY" \
        -d '{
            "Name": "测试�?,
            "URL": "https://example.com/video.mp4",
            "Container": "mp4",
            "Bitrate": 5000000
        }')

    SOURCE_ID=$(echo $SOURCE_CREATE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
    if [ ! -z "$SOURCE_ID" ]; then
        echo -e "${GREEN}�?{NC} 添加播放�? $SOURCE_ID"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        echo -e "${YELLOW}�?{NC} 添加播放源可能失败（未获取到 ID�?
    fi
fi
echo ""

# 12. PlaybackInfo
echo -e "${YELLOW}[12] 测试 PlaybackInfo...${NC}"
if [ ! -z "$ITEM_ID" ]; then
    test_endpoint "POST /emby/Items/{id}/PlaybackInfo" "POST" "/emby/Items/$ITEM_ID/PlaybackInfo" \
        '{"UserId":"'$USER_ID'","DeviceId":"test-device"}' "200" \
        "-H 'Content-Type: application/json' -H 'X-Emby-Token: $TOKEN'"
fi

# 13. 播放进度上报
echo -e "${YELLOW}[13] 测试播放进度上报...${NC}"
if [ ! -z "$ITEM_ID" ]; then
    test_endpoint "POST /emby/Sessions/Playing/Progress" "POST" "/emby/Sessions/Playing/Progress" \
        '{"ItemId":"'$ITEM_ID'","PositionTicks":1000000}' "204" \
        "-H 'Content-Type: application/json' -H 'X-Emby-Token: $TOKEN'"
fi

# 14. 标记为已�?echo -e "${YELLOW}[14] 测试标记已看...${NC}"
if [ ! -z "$ITEM_ID" ]; then
    test_endpoint "POST /emby/Users/{id}/PlayedItems/{id}" "POST" "/emby/Users/$USER_ID/PlayedItems/$ITEM_ID" "" "204" \
        "-H 'X-Emby-Token: $TOKEN'"
fi

# 15. 标记为收�?echo -e "${YELLOW}[15] 测试标记收藏...${NC}"
if [ ! -z "$ITEM_ID" ]; then
    test_endpoint "POST /emby/Users/{id}/FavoriteItems/{id}" "POST" "/emby/Users/$USER_ID/FavoriteItems/$ITEM_ID" "" "204" \
        "-H 'X-Emby-Token: $TOKEN'"
fi

# 16. 继续观看
echo -e "${YELLOW}[16] 测试继续观看...${NC}"
test_endpoint "GET /emby/Users/{id}/Items/Resume" "GET" "/emby/Users/$USER_ID/Items/Resume?Limit=10" "" "200" \
    "-H 'X-Emby-Token: $TOKEN'"

# 17. 获取统计信息
echo -e "${YELLOW}[17] 测试获取统计信息...${NC}"
test_endpoint "GET /api/admin/stats" "GET" "/api/admin/stats" "" "200" \
    "-H 'X-Api-Key: $ADMIN_API_KEY'"

# 清理
echo -e "${YELLOW}[18] 清理...${NC}"
if [ ! -z "$FAKEMBY_PID" ]; then
    kill $FAKEMBY_PID 2>/dev/null || true
    sleep 1
fi

# 最终报�?echo ""
echo "========================================"
echo "测试结果"
echo "========================================"
echo -e "${GREEN}通过: $TESTS_PASSED${NC}"
echo -e "${RED}失败: $TESTS_FAILED${NC}"
echo "========================================"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}�?所有测试通过�?{NC}"
    exit 0
else
    echo -e "${RED}�?有测试失�?{NC}"
    exit 1
fi
