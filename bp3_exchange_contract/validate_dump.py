# -*- coding: utf-8 -*-
"""Валидатор JSON-дампа обмена OneBase → БП3.

Проверяет, что дамп, который выгружает экспортёр OneBase, соответствует контракту
schema_dump.json и совместим с 1С-загрузчиком БП3. Это «эталонный» проверщик формата —
тот же, что использует основной репозиторий обмена.

Запуск:
    python validate_dump.py <путь_к_дампу.json> [ожидаемый_источник]

Зависимость для полной схемной проверки:  pip install jsonschema
(без неё проверяется только структурная совместимость; JSON Schema — пропускается).
"""
import io
import json
import os
import sys

ЗДЕСЬ = os.path.dirname(os.path.abspath(__file__))


def _load(p):
    return json.load(io.open(p, "r", encoding="utf-8-sig"))


def типы_транспорта(транспорт, правила):
    з = правила["транспорты"].get(транспорт)
    if з is None:
        raise KeyError("неизвестный транспорт: " + транспорт)
    return None if з.get("все") else list(з.get("типы", []))


def транспорт_поддерживает(транспорт, тип, правила):
    т = типы_транспорта(транспорт, правила)
    return True if т is None else (тип in т)


def проверить_совместимость(дамп, ожидаемый_источник, правила):
    """(совместим, проблемы, предупреждения). проблемы блокируют загрузку; предупреждения — нет."""
    проблемы, предупреждения = [], []
    if дамп.get("version") != "1.0":
        проблемы.append("version != '1.0': %r" % дамп.get("version"))
    if дамп.get("format") != "queryDump":
        проблемы.append("format != 'queryDump': %r" % дамп.get("format"))
    if not isinstance(дамп.get("queries"), dict) or not дамп.get("queries"):
        проблемы.append("queries отсутствует или пуст")
    n = дамп.get("настройки", {})
    src = n.get("источник", {}).get("конфигурация") or дамп.get("source")
    if src and ожидаемый_источник and src != ожидаемый_источник:
        проблемы.append("источник %r != ожидаемого %r" % (src, ожидаемый_источник))
    for тип in n.get("объекты", []):
        if not транспорт_поддерживает("JSON", тип, правила):
            предупреждения.append("тип %r не поддержан JSON-транспортом загрузчика — строки отсеются" % тип)
    return (len(проблемы) == 0, проблемы, предупреждения)


def main():
    if len(sys.argv) < 2:
        print("использование: python validate_dump.py <дамп.json> [ожидаемый_источник]")
        sys.exit(2)
    дамп = _load(sys.argv[1])
    ожид = sys.argv[2] if len(sys.argv) > 2 else None
    правила = _load(os.path.join(ЗДЕСЬ, "транспорты.json"))

    # 1) JSON Schema (полная проверка формата)
    try:
        import jsonschema
        схема = _load(os.path.join(ЗДЕСЬ, "schema_dump.json"))
        try:
            jsonschema.validate(дамп, схема)
            print("[schema] OK")
        except jsonschema.ValidationError as e:
            print("[schema] ОШИБКА:", (e.message or "")[:300])
            print("         путь:", list(e.absolute_path)[:6])
    except ImportError:
        print("[schema] пропущено (нет модуля jsonschema; pip install jsonschema)")

    # 2) Структурная совместимость с загрузчиком
    ok, probs, warns = проверить_совместимость(дамп, ожид, правила)
    print("[совместимость]", "OK" if ok else "ПРОБЛЕМЫ")
    for p in probs:
        print("   ! " + p)
    for w in warns:
        print("   ~ " + w)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
