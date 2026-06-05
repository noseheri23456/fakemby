#!/usr/bin/env python3
"""
FakEmby 测试数据导入脚本 (Python 版本)
导入 Big Buck Bunny 和其他测试媒体

使用方法:
    python3 import_test_data.py [--server http://localhost:8096] [--api-key admin-key]
"""

import requests
import json
import sys
from typing import Optional

class FakEmbyImporter:
    def __init__(self, server_url: str = "http://localhost:8096", api_key: str = "admin-key"):
        self.server_url = server_url.rstrip('/')
        self.api_key = api_key
        self.session = requests.Session()
        self.session.headers.update({
            "Content-Type": "application/json",
            "X-Api-Key": api_key
        })

    def create_item(self, data: dict) -> Optional[str]:
        """创建媒体项目，返回项目 ID"""
        url = f"{self.server_url}/api/admin/items"
        try:
            response = self.session.post(url, json=data)
            if response.status_code == 200:
                result = response.json()
                item_id = result.get('Id')
                if item_id:
                    return item_id
                # 尝试从响应中提取 ID
                if isinstance(result, dict) and 'Id' in result:
                    return result['Id']
            print(f"  ⚠ 状态码: {response.status_code}")
            return None
        except Exception as e:
            print(f"  ⚠ 错误: {e}")
            return None

    def add_source(self, item_id: str, name: str, url: str, container: str = "mp4",
                   bitrate: int = 5000000) -> bool:
        """为项目添加播放源"""
        api_url = f"{self.server_url}/api/admin/items/{item_id}/sources"
        data = {
            "Name": name,
            "URL": url,
            "Container": container,
            "Bitrate": bitrate
        }
        try:
            response = self.session.post(api_url, json=data)
            return response.status_code in [200, 204]
        except Exception as e:
            print(f"  ⚠ 添加源失败: {e}")
            return False

    def add_image(self, item_id: str, image_type: str, url: str) -> bool:
        """为项目添加图片"""
        # 注: 这个端点可能需要在 admin 层面支持
        # 目前通过 create_item 时传入 Images 字段
        return True

    def create_library(self, name: str, collection_type: str) -> Optional[str]:
        """创建媒体库"""
        print(f"创建库: {name}...")
        data = {
            "Name": name,
            "Type": "Folder",
            "CollectionType": collection_type
        }
        item_id = self.create_item(data)
        if item_id:
            print(f"  ✓ 库 ID: {item_id}")
        else:
            print(f"  ✗ 创建失败")
        return item_id

    def import_test_data(self):
        """导入完整的测试数据集"""
        print("=" * 50)
        print("FakEmby 测试数据导入")
        print("=" * 50)
        print()

        # 1. 创建电影库
        print("[1] 媒体库创建")
        movie_lib_id = self.create_library("电影库", "movies")
        if not movie_lib_id:
            print("✗ 无法创建电影库，退出")
            return
        print()

        # 2. 导入 Big Buck Bunny
        print("[2] 导入 Big Buck Bunny (1080p + 480p)")
        bbb_data = {
            "Name": "Big Buck Bunny",
            "Type": "Movie",
            "ParentId": movie_lib_id,
            "Overview": "Big Buck Bunny is a short animated comedy film about a large rabbit dealing with three tiny creatures building a dam on his land. It's a great test video.",
            "Year": 2008,
            "PremiereDate": "2008-04-10",
            "CommunityRating": 7.8,
            "OfficialRating": "PG",
            "RuntimeTicks": 600000000000,  # 10 分钟 (100-nanosecond units)
            "Genres": ["Animation", "Comedy", "Short"],
            "Tags": ["Open Source", "Blender", "Test"]
        }
        bbb_id = self.create_item(bbb_data)
        if bbb_id:
            print(f"  ✓ 项目 ID: {bbb_id}")

            # 添加 1080p 源
            print("  添加 1080p 源...")
            if self.add_source(bbb_id, "1080p (H.264)",
                            "https://peach.blender.org/download/bbb_sunflower_1080p_h264.mov",
                            "mov", 5000000):
                print("    ✓ 1080p 源添加成功")

            # 添加 480p 源
            print("  添加 480p 源...")
            if self.add_source(bbb_id, "480p (H.264)",
                            "https://peach.blender.org/download/bbb_sunflower_480p_h264.mov",
                            "mov", 1000000):
                print("    ✓ 480p 源添加成功")
        else:
            print("  ✗ 创建项目失败")
        print()

        # 3. 创建电视剧库
        print("[3] 创建电视剧库")
        tv_lib_id = self.create_library("电视剧库", "tvshows")
        if not tv_lib_id:
            print("✗ 无法创建电视剧库")
            return
        print()

        # 4. 创建电视剧
        print("[4] 创建示例电视剧 (Test Series)")
        series_data = {
            "Name": "Test Series",
            "Type": "Series",
            "ParentId": tv_lib_id,
            "Overview": "这是一个测试电视剧系列，用于验证 Season/Episode 层级",
            "Year": 2024,
            "Genres": ["Drama", "Test"]
        }
        series_id = self.create_item(series_data)
        if not series_id:
            print("✗ 创建电视剧失败")
            return
        print(f"  ✓ 电视剧 ID: {series_id}")
        print()

        # 5. 创建第一季
        print("[5] 创建 Season 1")
        season_data = {
            "Name": "Season 1",
            "Type": "Season",
            "ParentId": series_id,
            "SeasonNumber": 1
        }
        season_id = self.create_item(season_data)
        if not season_id:
            print("✗ 创建 Season 失败")
            return
        print(f"  ✓ Season ID: {season_id}")
        print()

        # 6. 创建剧集
        print("[6] 创建 Episode 1 & 2")
        for ep_num in [1, 2]:
            ep_data = {
                "Name": f"Episode {ep_num}",
                "Type": "Episode",
                "ParentId": season_id,
                "SeasonNumber": 1,
                "EpisodeNumber": ep_num,
                "Overview": f"测试剧集 {ep_num}",
                "RuntimeTicks": 300000000000  # 5 分钟
            }
            ep_id = self.create_item(ep_data)
            if ep_id:
                print(f"  ✓ Episode {ep_num} ID: {ep_id}")

                # 添加播放源
                if self.add_source(ep_id, "Test Stream",
                                "https://peach.blender.org/download/bbb_sunflower_480p_h264.mov",
                                "mov"):
                    print(f"    ✓ 播放源添加成功")
            else:
                print(f"  ✗ 创建 Episode {ep_num} 失败")
        print()

        # 完成
        print("=" * 50)
        print("✓ 测试数据导入完成！")
        print("=" * 50)
        print()
        print("创建的结构:")
        print(f"  电影库 ({movie_lib_id})")
        print(f"    └─ Big Buck Bunny ({bbb_id})")
        print(f"       ├─ 1080p 源")
        print(f"       └─ 480p 源")
        print()
        print(f"  电视剧库 ({tv_lib_id})")
        print(f"    └─ Test Series ({series_id})")
        print(f"      └─ Season 1 ({season_id})")
        print(f"        ├─ Episode 1")
        print(f"        └─ Episode 2")
        print()
        print("下一步:")
        print("  1. 用小幻影视/SenPlayer 连接到服务器")
        print("  2. 登录 (admin/admin)")
        print("  3. 浏览"电影库"查看 Big Buck Bunny")
        print("  4. 浏览"电视剧库"查看电视剧")
        print("  5. 点击播放测试")
        print()

def main():
    import argparse

    parser = argparse.ArgumentParser(description="FakEmby 测试数据导入")
    parser.add_argument("--server", default="http://localhost:8096",
                       help="服务器地址 (默认: http://localhost:8096)")
    parser.add_argument("--api-key", default="admin-key",
                       help="管理员 API key (默认: admin-key)")

    args = parser.parse_args()

    # 检查服务器连接
    try:
        response = requests.get(f"{args.server}/emby/System/Info/Public", timeout=5)
        if response.status_code != 200:
            print(f"✗ 服务器响应异常: {response.status_code}")
            print("  请确保 FakEmby 服务器正在运行")
            sys.exit(1)
    except requests.ConnectionError:
        print(f"✗ 无法连接到服务器: {args.server}")
        print("  请确保 FakEmby 服务器正在运行:")
        print("    go run ./cmd/fakemby")
        print("  或")
        print("    docker-compose up -d")
        sys.exit(1)
    except Exception as e:
        print(f"✗ 连接错误: {e}")
        sys.exit(1)

    # 导入数据
    importer = FakEmbyImporter(args.server, args.api_key)
    importer.import_test_data()

if __name__ == "__main__":
    main()
