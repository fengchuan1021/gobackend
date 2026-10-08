#!/usr/bin/env python3
import json
import random
import sys

from faker import Faker


def pick_faker(language):
    raw = (language or "").strip().replace("-", "_")
    candidates = []
    if raw:
        candidates.append(raw)
        parts = [part for part in raw.split("_") if part]
        if len(parts) >= 2:
            candidates.append(parts[0] + "_" + parts[-1])
        if parts:
            candidates.append(parts[0])
    candidates.append("zh_CN")
    seen = set()
    for locale in candidates:
        if locale in seen:
            continue
        seen.add(locale)
        try:
            return Faker(locale)
        except Exception:
            continue
    return Faker()


def main():
    language = sys.argv[1] if len(sys.argv) > 1 else "zh_CN"
    fake = pick_faker(language)
    contacts = []
    for _ in range(random.randint(10, 20)):
        contacts.append(
            {
                "name": fake.name(),
                "phone": fake.phone_number(),
                "address": " ".join(fake.address().split()),
                "email": fake.email(),
            }
        )
    json.dump(contacts, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    main()
