#!/bin/bash

# FakEmby 娴嬭瘯鏁版嵁瀵煎叆鑴氭湰
# 瀵煎叆 Big Buck Bunny 鍜屽叾浠栨祴璇曞獟浣?
set -e

BASE_URL="http://localhost:8096"
ADMIN_API_KEY="admin-key"

echo "========================================"
echo "FakEmby 娴嬭瘯鏁版嵁瀵煎叆"
echo "========================================"
echo ""

# 1. 鍒涘缓鐢靛奖搴?echo "[1] 鍒涘缓鐢靛奖搴?.."
LIB_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "鐢靛奖搴?,
    "Type": "Folder",
    "CollectionType": "movies"
  }')

LIB_ID=$(echo $LIB_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "鉁?鐢靛奖搴?ID: $LIB_ID"
echo ""

# 2. 瀵煎叆 Big Buck Bunny
echo "[2] 瀵煎叆 Big Buck Bunny..."
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
echo "鉁?椤圭洰 ID: $ITEM_ID"
echo ""

# 3. 娣诲姞鎾斁婧?(Big Buck Bunny 1080p)
echo "[3] 娣诲姞鎾斁婧?(1080p)..."
curl -s -X POST "$BASE_URL/api/admin/items/$ITEM_ID/sources" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "1080p (MP4)",
    "URL": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_1080p_h264.mov",
    "Container": "mov",
    "Bitrate": 5000000
  }' > /dev/null
echo "鉁?1080p 婧愭坊鍔?

# 4. 娣诲姞鎾斁婧?(480p 鐗堟湰)
echo "[4] 娣诲姞鎾斁婧?(480p)..."
curl -s -X POST "$BASE_URL/api/admin/items/$ITEM_ID/sources" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "480p (MP4)",
    "URL": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_480p_h264.mov",
    "Container": "mov",
    "Bitrate": 1000000
  }' > /dev/null
echo "鉁?480p 婧愭坊鍔?
echo ""

# 5. 娣诲姞娴锋姤
echo "[5] 娣诲姞娴锋姤..."
curl -s -X POST "$BASE_URL/api/admin/items/$ITEM_ID/images" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Type": "Primary",
    "URL": "https://peach.blender.org/download/bbb_poster.jpg"
  }' > /dev/null 2>&1 || echo "鈿?娴锋姤娣诲姞鍙兘澶辫触"
echo "鉁?娴锋姤娣诲姞"
echo ""

# 6. 鍒涘缓鐢佃鍓у簱
echo "[6] 鍒涘缓鐢佃鍓у簱..."
TV_LIB_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "鐢佃鍓у簱",
    "Type": "Folder",
    "CollectionType": "tvshows"
  }')

TV_LIB_ID=$(echo $TV_LIB_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "鉁?鐢佃鍓у簱 ID: $TV_LIB_ID"
echo ""

# 7. 鍒涘缓绀轰緥鐢佃鍓?echo "[7] 鍒涘缓绀轰緥鐢佃鍓?(Test Series)..."
SERIES_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Test Series",
    "Type": "Series",
    "ParentId": "'$TV_LIB_ID'",
    "Overview": "杩欐槸涓€涓祴璇曠數瑙嗗墽绯诲垪",
    "Year": 2024,
    "Genres": ["Test", "Drama"]
  }')

SERIES_ID=$(echo $SERIES_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "鉁?鐢佃鍓?ID: $SERIES_ID"
echo ""

# 8. 鍒涘缓绗竴瀛?echo "[8] 鍒涘缓绗竴瀛?.."
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
echo "鉁?绗竴瀛?ID: $SEASON_ID"
echo ""

# 9. 鍒涘缓绗竴闆?echo "[9] 鍒涘缓绗竴闆?.."
EP_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Episode 1",
    "Type": "Episode",
    "ParentId": "'$SEASON_ID'",
    "SeasonNumber": 1,
    "EpisodeNumber": 1,
    "Overview": "娴嬭瘯鍓ч泦 1",
    "RuntimeTicks": 300000000000
  }')

EP_ID=$(echo $EP_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "鉁?绗竴闆?ID: $EP_ID"
echo ""

# 10. 涓虹涓€闆嗘坊鍔犳挱鏀炬簮
echo "[10] 涓虹涓€闆嗘坊鍔犳挱鏀炬簮..."
curl -s -X POST "$BASE_URL/api/admin/items/$EP_ID/sources" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Test Episode Stream",
    "URL": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_1080p_h264.mov",
    "Container": "mov"
  }' > /dev/null
echo "鉁?鎾斁婧愭坊鍔?
echo ""

# 11. 鍒涘缓绗簩闆?echo "[11] 鍒涘缓绗簩闆?.."
EP2_RESPONSE=$(curl -s -X POST "$BASE_URL/api/admin/items" \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: $ADMIN_API_KEY" \
  -d '{
    "Name": "Episode 2",
    "Type": "Episode",
    "ParentId": "'$SEASON_ID'",
    "SeasonNumber": 1,
    "EpisodeNumber": 2,
    "Overview": "娴嬭瘯鍓ч泦 2",
    "RuntimeTicks": 300000000000
  }')

EP2_ID=$(echo $EP2_RESPONSE | grep -o '"Id":"[^"]*' | cut -d'"' -f4 | head -1)
echo "鉁?绗簩闆?ID: $EP2_ID"
echo ""

echo "========================================"
echo "鉁?娴嬭瘯鏁版嵁瀵煎叆瀹屾垚锛?
echo "========================================"
echo ""
echo "鍒涘缓鐨勯」鐩細"
echo "  - 鐢靛奖搴? $LIB_ID"
echo "    鈹斺攢 Big Buck Bunny: $ITEM_ID"
echo ""
echo "  - 鐢佃鍓у簱: $TV_LIB_ID"
echo "    鈹斺攢 Test Series: $SERIES_ID"
echo "      鈹斺攢 Season 1: $SEASON_ID"
echo "        鈹溾攢 Episode 1: $EP_ID"
echo "        鈹斺攢 Episode 2: $EP2_ID"
echo ""
echo "鐜板湪鍙互浣跨敤浠ヤ笅 URL 璁块棶鏈嶅姟鍣細"
echo "  - 娴忚鍣? http://localhost:8096"
echo "  - 灏忓够褰辫: 娣诲姞鏈嶅姟鍣?http://localhost:8096"
echo "  - SenPlayer: 娣诲姞鏈嶅姟鍣?http://localhost:8096"
echo ""
echo "榛樿鐧诲綍锛?
echo "  鐢ㄦ埛鍚? admin"
echo "  瀵嗙爜: admin"
echo ""
