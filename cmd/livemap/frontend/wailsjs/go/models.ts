export namespace appconfig {
	
	export class MapMatching {
	    world_map_path: string;
	    global_minimap_scale: number;
	    global_map_scale: number;
	    global_workers: number;
	    global_timeout_ms: number;
	    local_workers: number;
	    local_roi: number;
	    local_expanded_roi: number;
	    match_threshold: number;
	    global_search_hotkey: string;
	
	    static createFrom(source: any = {}) {
	        return new MapMatching(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.world_map_path = source["world_map_path"];
	        this.global_minimap_scale = source["global_minimap_scale"];
	        this.global_map_scale = source["global_map_scale"];
	        this.global_workers = source["global_workers"];
	        this.global_timeout_ms = source["global_timeout_ms"];
	        this.local_workers = source["local_workers"];
	        this.local_roi = source["local_roi"];
	        this.local_expanded_roi = source["local_expanded_roi"];
	        this.match_threshold = source["match_threshold"];
	        this.global_search_hotkey = source["global_search_hotkey"];
	    }
	}
	export class MapResource {
	    path: string;
	    game: string;
	    map_version: string;
	    selected: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MapResource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.game = source["game"];
	        this.map_version = source["map_version"];
	        this.selected = source["selected"];
	    }
	}
	export class PinResource {
	    path: string;
	    name: string;
	    game: string;
	    map_version: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PinResource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.game = source["game"];
	        this.map_version = source["map_version"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Resources {
	    maps: MapResource[];
	    pins: PinResource[];
	
	    static createFrom(source: any = {}) {
	        return new Resources(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maps = this.convertValues(source["maps"], MapResource);
	        this.pins = this.convertValues(source["pins"], PinResource);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MinimapRegion {
	    x: number;
	    y: number;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new MinimapRegion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.size = source["size"];
	    }
	}
	export class Config {
	    process_name: string;
	    backend: string;
	    fps: number;
	    minimap_region: MinimapRegion;
	    map_matching: MapMatching;
	    resources: Resources;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.process_name = source["process_name"];
	        this.backend = source["backend"];
	        this.fps = source["fps"];
	        this.minimap_region = this.convertValues(source["minimap_region"], MinimapRegion);
	        this.map_matching = this.convertValues(source["map_matching"], MapMatching);
	        this.resources = this.convertValues(source["resources"], Resources);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace gui {
	
	export class DebugStatus {
	    inspectWindow: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DebugStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inspectWindow = source["inspectWindow"];
	    }
	}
	export class MatchTestStatus {
	    running: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new MatchTestStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.message = source["message"];
	    }
	}
	export class Status {
	    running: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.message = source["message"];
	    }
	}

}
