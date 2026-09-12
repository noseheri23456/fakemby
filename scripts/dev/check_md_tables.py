import re

bad = 0
with open('docs/ROADMAP.md', encoding='utf-8') as f:
    for i, line in enumerate(f, 1):
        line = line.rstrip('\n')
        if not re.match(r'^\| M\d+-\d+', line):
            continue
        # 先剔除已转义的 \|，再按剩余竖线分列
        cols = len(re.split(r'\|', line.replace(r'\|', '@'))) - 2
        if cols < 3 or cols > 5:
            print(f'{i}: {cols}列 -> {line[:70]}')
            bad += 1
print('OK 无结构异常' if bad == 0 else f'{bad} 行异常')
