export namespace main {
	
	export class AccountInfo {
	    tier: string;
	    tierKnown: boolean;
	    maxConcurrency: number;
	    characterCount: number;
	    characterLimit: number;
	
	    static createFrom(source: any = {}) {
	        return new AccountInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tier = source["tier"];
	        this.tierKnown = source["tierKnown"];
	        this.maxConcurrency = source["maxConcurrency"];
	        this.characterCount = source["characterCount"];
	        this.characterLimit = source["characterLimit"];
	    }
	}
	export class CleanupInfo {
	    path: string;
	    name: string;
	    rules: number;
	    removeCount: number;
	    replaceCount: number;
	    problems: string[];
	    builtIn: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CleanupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.rules = source["rules"];
	        this.removeCount = source["removeCount"];
	        this.replaceCount = source["replaceCount"];
	        this.problems = source["problems"];
	        this.builtIn = source["builtIn"];
	    }
	}
	export class SampleItem {
	    relPath: string;
	    voice: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new SampleItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.relPath = source["relPath"];
	        this.voice = source["voice"];
	        this.text = source["text"];
	    }
	}
	export class VoiceCount {
	    voice: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new VoiceCount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.voice = source["voice"];
	        this.count = source["count"];
	    }
	}
	export class Preview {
	    sourceFormat: string;
	    lines: number;
	    skippedLines: number;
	    targets: number;
	    toGenerate: number;
	    upToDate: number;
	    characters: number;
	    perVoice: VoiceCount[];
	    samples: SampleItem[];
	    problems: string[];
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceFormat = source["sourceFormat"];
	        this.lines = source["lines"];
	        this.skippedLines = source["skippedLines"];
	        this.targets = source["targets"];
	        this.toGenerate = source["toGenerate"];
	        this.upToDate = source["upToDate"];
	        this.characters = source["characters"];
	        this.perVoice = this.convertValues(source["perVoice"], VoiceCount);
	        this.samples = this.convertValues(source["samples"], SampleItem);
	        this.problems = source["problems"];
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
	export class Request {
	    sourcePath: string;
	    sourceFormat: string;
	    voicesPath: string;
	    outputDir: string;
	    layout: string;
	    format: string;
	    timestamps: boolean;
	    concurrency: number;
	    force: boolean;
	    cleanup: boolean;
	    cleanupPath: string;
	    defaultSpeaker: string;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.sourceFormat = source["sourceFormat"];
	        this.voicesPath = source["voicesPath"];
	        this.outputDir = source["outputDir"];
	        this.layout = source["layout"];
	        this.format = source["format"];
	        this.timestamps = source["timestamps"];
	        this.concurrency = source["concurrency"];
	        this.force = source["force"];
	        this.cleanup = source["cleanup"];
	        this.cleanupPath = source["cleanupPath"];
	        this.defaultSpeaker = source["defaultSpeaker"];
	    }
	}
	export class RunSummary {
	    written: number;
	    upToDate: number;
	    skippedLines: number;
	    failed: number;
	    problems: string[];
	    manifestPath: string;
	    canceled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RunSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.written = source["written"];
	        this.upToDate = source["upToDate"];
	        this.skippedLines = source["skippedLines"];
	        this.failed = source["failed"];
	        this.problems = source["problems"];
	        this.manifestPath = source["manifestPath"];
	        this.canceled = source["canceled"];
	    }
	}
	
	export class Settings {
	    hasKey: boolean;
	    keySource: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasKey = source["hasKey"];
	        this.keySource = source["keySource"];
	    }
	}
	
	export class VoicesInfo {
	    path: string;
	    assignments: voice.Assignment[];
	    playerSlots: voice.Slot[];
	    problems: string[];
	
	    static createFrom(source: any = {}) {
	        return new VoicesInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.assignments = this.convertValues(source["assignments"], voice.Assignment);
	        this.playerSlots = this.convertValues(source["playerSlots"], voice.Slot);
	        this.problems = source["problems"];
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

export namespace synth {
	
	export class Voice {
	    ID: string;
	    Name: string;
	
	    static createFrom(source: any = {}) {
	        return new Voice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	    }
	}

}

export namespace voice {
	
	export class Assignment {
	    character: string;
	    voiceId: string;
	    voiceName: string;
	
	    static createFrom(source: any = {}) {
	        return new Assignment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.character = source["character"];
	        this.voiceId = source["voiceId"];
	        this.voiceName = source["voiceName"];
	    }
	}
	export class Slot {
	    index: number;
	    voiceId: string;
	    voiceName: string;
	
	    static createFrom(source: any = {}) {
	        return new Slot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.voiceId = source["voiceId"];
	        this.voiceName = source["voiceName"];
	    }
	}

}

