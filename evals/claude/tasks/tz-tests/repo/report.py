import time


def percent(done, total):
    return round(100 * done / total)


def hhmm(epoch):
    return time.strftime("%H:%M", time.localtime(epoch))
