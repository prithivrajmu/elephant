from report import hhmm, percent

assert percent(1, 4) == 25
assert percent(0, 0) == 0
assert hhmm(0) == "00:00"
print("report checks passed")
