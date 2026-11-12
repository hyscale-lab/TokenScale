from vllm.coldstart_profiler import ColdstartProfiler, DummyProfiler, ENVSTR
from multiprocessing import Manager
import os
from typing import List
from vllm.logger import init_logger
logger = init_logger(__name__)


MANAGER = None
PROFILER_MAP = None

def _init_mp_state():
    global MANAGER, PROFILER_MAP
    MANAGER = Manager()
    PROFILER_MAP = MANAGER.dict()

def _create_profiler(name: str = "root") -> ColdstartProfiler:
    val = os.getenv(ENVSTR)
    if val is not None:
        profiler = ColdstartProfiler(name=name, manager=MANAGER)
    else:
        profiler = DummyProfiler(name=name, manager=MANAGER)
    PROFILER_MAP[name] = profiler
    return profiler

def create_profiler(name: str = "root") -> ColdstartProfiler:
    _init_mp_state()
    return _create_profiler(name)

def fork_profiler(name: str, parent_name: str="root") -> ColdstartProfiler:
    parent_profiler = PROFILER_MAP[parent_name]
    child_profiler = _create_profiler(name)
    parent_profiler.fork(name, child_profiler)
    return child_profiler

def get_profiler(name: str = "root") -> ColdstartProfiler:
    try:
        profiler = PROFILER_MAP[name]
    except Exception as e:
       logger.error(f"failed to get profiler {name}: current profilers: {PROFILER_MAP.keys()}") 
       raise e
    return profiler

def get_profiler_names() -> List[str]:
    profiler_names = [k for k in PROFILER_MAP.keys()]
    return profiler_names