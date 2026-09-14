# -*- coding: utf-8 -*-
"""列出 Windows MySQL cs_biz_db 的表与行数（迁移前勘察）"""
import pymysql

conn = pymysql.connect(host='127.0.0.1', port=3306, user='root', password='123456',
                       charset='utf8mb4', connect_timeout=5)
cur = conn.cursor()
cur.execute("SHOW DATABASES LIKE 'cs_biz_db'")
print('cs_biz_db 存在:', cur.fetchone() is not None)
cur.execute("USE cs_biz_db")
cur.execute("SHOW TABLES")
tables = [r[0] for r in cur.fetchall()]
print('表清单:', tables)
for t in tables:
    cur.execute("SELECT COUNT(*) FROM `%s`" % t)
    print('  %-20s %d 行' % (t, cur.fetchone()[0]))
conn.close()
