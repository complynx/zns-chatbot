
class Masseur:
    def __init__(self, id: int, repr: str) -> None:
        self.id = id
        self.name, self.last_name, self.icon = repr.split(" ")

class Massage:
    def __init__(self, price: int, time: int, name: str) -> None:
        self.price = price
        self.time = time
        self.name = name

MASSEURS = [
    Masseur(5907421587, "Максим Тарасов 🧔🏻"),
    Masseur(188815729, "Антон Корелин 👨🏻‍🦰"),
    Masseur(1518045050, "Таисия Потапова 👩🏻"),
    Masseur(272705905, "Екатерина Шайн 👩🏻‍🦰"),
]
BUFFER_TIME = 5 # min
MASSAGES = [ # price, time in minutes, name
    Massage( 500, 20, "локальный массаж"),
    Massage(1000, 30, "спина+руки или ноги"),
    Massage(1350, 45, "несколько зон"),
    Massage(1500, 60, "общий массаж"),
]

class MassageSystem:
    pass
