import pytest

from app.core import fib, slugify


def test_slugify_basic():
    assert slugify("Hello, World!") == "hello-world"


def test_slugify_collapses_separators():
    assert slugify("  Foo   Bar__Baz  ") == "foo-bar-baz"


def test_slugify_unicode_kept():
    # Python's str.isalnum() is Unicode-aware, so accented letters are kept.
    assert slugify("café crème") == "café-crème"


def test_fib_sequence():
    assert [fib(i) for i in range(7)] == [0, 1, 1, 2, 3, 5, 8]


def test_fib_negative_raises():
    with pytest.raises(ValueError):
        fib(-1)
