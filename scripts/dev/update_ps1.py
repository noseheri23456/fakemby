import json
import os
import re

json_payload_movies = {
    "library": "Movies",
    "items": [
        {
            "name": "Big Buck Bunny",
            "type": "Movie",
            "overview": "Big Buck Bunny is a short animated comedy film about a large rabbit dealing with three tiny creatures. Created by the Blender Foundation.",
            "year": 2008,
            "premiere_date": "2008-04-10",
            "community_rating": 7.8,
            "official_rating": "PG",
            "runtime_minutes": 10,
            "genres": ["Animation", "Comedy", "Short"],
            "studios": ["Blender Foundation"],
            "taglines": ["A big rabbit adventure"],
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
            "people": [{"name": "花衣つばき", "type": "Actor", "role": ""}],
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

json_payload_tv = {
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
                        {
                            "name": "Pilot",
                            "episode_number": 1,
                            "overview": "The first episode of the test series.",
                            "sources": [{"name": "HD", "url": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_480p_h264.mov", "container": "mov"}]
                        },
                        {
                            "name": "Episode 2",
                            "episode_number": 2,
                            "overview": "The second episode.",
                            "sources": [{"name": "HD", "url": "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_480p_h264.mov", "container": "mov"}]
                        }
                    ]
                }
            ]
        }
    ]
}

movies_str = json.dumps(json_payload_movies, separators=(',', ':'), ensure_ascii=False)
tv_str = json.dumps(json_payload_tv, separators=(',', ':'), ensure_ascii=False)

ps1_file = "f:/codx/fakemby/scripts/test/start-test-server.ps1"
with open(ps1_file, "r", encoding="utf-8-sig") as f:
    content = f.read()

content = content.replace('$ProjectDir = Split-Path -Parent $MyInvocation.MyCommand.Path\nSet-Location $ProjectDir', 
                          '$ProjectDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)\nSet-Location $ProjectDir')

content = content.replace('go build -o fakemby.exe ./cmd/fakemby', 'go build -o fakemby.exe ./cmd/fakemby')

content = re.sub(r'-d \'.*?\' 2>&1', lambda m: f"-d '{movies_str}' 2>&1" if 'Movies' in m.group(0) else f"-d '{tv_str}' 2>&1", content)

with open(ps1_file, "w", encoding="utf-8-sig") as f:
    f.write(content)

print("Updated PS1 successfully.")
