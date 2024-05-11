from typing import Any, Dict
from motor.core import AgnosticCollection

class Storage(object):
    _bot_id = None
    _storage = None
    _collection: AgnosticCollection
    def __init__(self, collection: AgnosticCollection) -> None:
        self._collection = collection

    async def refresh(self, bot_id: int):
        self._bot_id = bot_id
        storage = await self._collection.find_one({
            "bot_id": self._bot_id,
        })
        self._storage = dict()
        if storage is not None:
            if "vars" in storage:
                self._storage = storage["vars"]
    
    def __len__(self):
        return len(self._storage)
    
    def __getitem__(self, key: str) -> Any:
        return self._storage[key]
    
    def __iter__(self):
        return iter(self._storage)
    
    def __contains__(self, item):
        return item in self._storage
    
    def __setitem__(self, key: str, val: Any) -> Any:
        raise Exception("use await Storage.set(key, val)")
    
    def __delitem__(self, key: str) -> Any:
        raise Exception("use await Storage.del_key(key)")
    
    async def save(self):
        await self._collection.update_one({
            "bot_id": self._bot_id,
        }, {
            "$set": {
                "vars": self._storage,
            },
            "$setOnInsert": {
                "bot_id": self._bot_id,
            }
        }, upsert=True)

    async def set(self, key: str, val: Any):
        self._storage[key] = val
        await self.save()

    async def del_key(self, key: str):
        del self._storage[key]
        await self.save()


