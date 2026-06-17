#!/usr/bin/env python3
"""FakEmby test data importer using /api/admin/import batch API"""
import requests, json, sys

SERVER = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:9096"
API_KEY = sys.argv[2] if len(sys.argv) > 2 else "change-me"

headers = {"Content-Type": "application/json", "X-Api-Key": API_KEY}

data = {
    "library": "Movies",
    "items": [
        {
            "name": "Big Buck Bunny",
            "type": "Movie",
            "overview": "Big Buck Bunny is a short animated comedy film about a large rabbit dealing with three tiny creatures.",
            "year": 2008,
            "premiere_date": "2008-04-10",
            "community_rating": 7.8,
            "official_rating": "PG",
            "runtime_minutes": 10,
            "genres": ["Animation", "Comedy", "Short"],
            "tags": ["Open Source", "Blender", "Test"],
            "taglines": ["A big rabbit adventure"],
            "studios": ["Blender Foundation"],
            "people": [{"name": "Sacha Goedegebure", "type": "Director", "role": ""}],
            "sources": [
                {"name": "1080p (H.264)", "url": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_1080p_h264.mov", "container": "mov", "bitrate": 5000000},
                {"name": "480p (H.264)", "url": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_480p_h264.mov", "container": "mov", "bitrate": 1000000}
            ],
            "images": {
                "Primary": "https://upload.wikimedia.org/wikipedia/commons/c/c5/Big_buck_bunny_poster_big.jpg",
                "Backdrop": "https://upload.wikimedia.org/wikipedia/commons/7/7d/Big_Buck_Bunny_screenshot.jpg"
            }
        },
        {
            "name": "NMSL-037 深夜のコンビニで寝盗られ堕とされた超キメパコ絶頂のヨガパンツ奥さん 花衣つばき",
            "type": "Movie",
            "overview": "配信開始日 2026-04-02",
            "year": 2026,
            "premiere_date": "2026-04-02",
            "official_rating": "JP-18+",
            "genres": ["3P・4P", "4K", "単体作品", "乱交", "寝取り・寝取られ", "人妻", "中出し", "アクメ・オーガズム", "ドラマ", "ハイビジョン"],
            "studios": ["MARLO MEDIA"],
            "tags": ["MARLO MEDIA"],
            "people": [{"name": "花衣つばき", "type": "Actor", "role": "", "image_url": "https://fastcdn.dpdns.org/nmsl/nmsl-037/nmsl-037-actor.jpg"}],
            "sources": [
                {"name": "Direct (H.264)", "url": "https://ftstrm.fastcdn.dpdns.org/video/105741", "container": "mp4"}
            ],
            "images": {
                "Primary": "http://fastcdn.dpdns.org/nmsl/nmsl-037/nmsl-037-poster.jpg",
                "Backdrop": "http://fastcdn.dpdns.org/nmsl/nmsl-037/nmsl-037-fanart.jpg",
                "Logo": "http://fastcdn.dpdns.org/nmsl/nmsl-037/nmsl-037-landscape.jpg"
            }
        }
    ]
}

data2 = {
    "library": "TV Shows",
    "items": [
        {
            "name": "Test Series",
            "type": "Series",
            "overview": "A test TV series for verifying Season/Episode hierarchy",
            "year": 2024,
            "genres": ["Drama", "Test"],
            "seasons": [
                {
                    "season_number": 1,
                    "episodes": [
                        {"name": "Pilot", "episode_number": 1, "overview": "The first episode", "sources": [{"name": "HD", "url": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_480p_h264.mov", "container": "mov"}]},
                        {"name": "Episode 2", "episode_number": 2, "overview": "The second episode", "sources": [{"name": "HD", "url": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_480p_h264.mov", "container": "mov"}]}
                    ]
                }
            ]
        }
    ]
}

print("Importing...")
r1 = requests.post(f"{SERVER}/api/admin/import", json=data, headers=headers)
print(f"Movies: {r1.json()}")
r2 = requests.post(f"{SERVER}/api/admin/import", json=data2, headers=headers)
print(f"TV Shows: {r2.json()}")
print("Done.")
