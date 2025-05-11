from .assistant import Assistant
from .avatar import Avatar
from .auth import Auth
from .food import Food
from .superuser import Superuser
# from .orders import Orders
# from .massage import MassagePlugin
from .passes import Passes

plugins = [
    Auth,
    Assistant,
    Avatar,
    Food,
    Superuser,
    # Orders,
    # MassagePlugin,
    Passes,
]
