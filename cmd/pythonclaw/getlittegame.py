import re
import time
import requests
import pymysql
import os
from urllib.parse import urlparse, parse_qs
if os.environ.get('DEBUG'):
    conn=pymysql.connect(host='192.168.1.231',port=3306,user='root',password='yangyongli8977',database='antares')
else:
    conn=pymysql.connect(host='127.0.0.1',port=3306,user='root',password='yangyongli89771021',database='antares')
session=requests.Session()
session.headers={
    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
}


def extract_package_name(download_url):
    query = parse_qs(urlparse(download_url).query)
    pkg_list = query.get('pkg', [])
    if not pkg_list:
        return ''
    return pkg_list[0]


def save_random_little_apk(package_name, download_url):
    if not package_name or not download_url:
        return

    with conn.cursor() as cursor:
        cursor.execute(
            """
            INSERT INTO random_little_apk (package_name, download_url)
            VALUES (%s, %s)
            ON DUPLICATE KEY UPDATE
                download_url = VALUES(download_url),
                updated_at = CURRENT_TIMESTAMP
            """,
            (package_name, download_url)
        )
    conn.commit()

for i in range(10):
    result = []
    try:
        url=f'https://www.wandoujia.com/wdjweb/api/category/more?catId=6001&subCatId=0&page={i+1}&ctoken=Nh8zA6j4wLI3tFwCs2yGjy1O'
        response=session.get(url)
        data=response.json()

        html = data["data"]["content"]
        items = re.findall(
            r'data-appid="(\d+)".*?<span title="([\d.]+)MB">',
            html,
            re.S
        )
        for appid, size in items:
            if float(size) < 100:
                result.append(appid)
    except Exception as e:
        pass
    time.sleep(2)
    for appid in result:
        print(appid)
        resp = session.get(
            f'https://www.wandoujia.com/apps/{appid}/download/dot?ch=detail_normal_dl',
            allow_redirects=False
        )
        download_url = resp.headers.get('Location')
        if download_url:
            package_name = extract_package_name(download_url)
            if package_name:
                save_random_little_apk(package_name, download_url)
                print(package_name, download_url)