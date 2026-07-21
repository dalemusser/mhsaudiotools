remove_items = ["[em1]", "[/em1]", "[em2]", "[/em2]", "[em3]", "[/em3]", "[em4]", "[/em4]", "[em5]", "[/em5]", "[em6]", "[/em6]", "\\r",
"[/r]", "[/n]", "\\n", "<joke>", "[var=classifierFeedback]", "...",
"{{PLACEHOLDER - CLOSE MAP}}", "{{PLACEHOLDER - OPEN MAP MENU}}", "{{PLACEHOLDER - MAP OPENS, MAP TUTORIAL 2 PLAYS}}",
"{{PLACEHOLDER - ARGUMENTATION INTERFACE OPENS}}", "{{PLACEHOLDER - DRONE CONTROL TUTORIAL}}",
"{{PLACEHOLDER - DATA TABLE POP-UP}}", "{{PLACEHOLDER - TOPOGRAPHY VIDEO}}", "{{PLACEHOLDER - U1 TOPOGRAPHY LESSON PLAYS}}",
"{{PLACEHOLDER - U1 TOPOGRAPHY LESSON PLAYS}}", "{{PLACEHOLDER - WATERSHED TOPPO LESSON}}", "{{PLACEHOLDER - WATERSHED TOPPO LESSON PLAYS}}",
"{{PLACEHOLDER - FORGE MINI GAME}}", "{{PLACEHOLDER - MAP OPENS AUTOMATICALLY}}", "{{PLACEHOLDER - MAP OPENS}}",
"{{PLACEHOLDER - ARGUMENTATION}}", "{{PLACEHOLDER - TRANSITION TO BASE CAMP}}", "{{PLACEHOLDER - CLASSIFICATION EXERCISE AND FEEDBACK}}",
"{{PLACEHOLDER - LAUNCH DRONE}}", "{{PLACEHOLDER - DANI MENU ACTIVATION ANIMATION}}", "{{PLACEHOLDER - MENU OPENS AUTOMATICALLY}}",
"{{PLACEHOLDER - PLAYER CLOSES MENU}}", "{{PLACEHOLDER - ARGUMENTATION INTERFACE OPENS}}", "{{PLACEHOLDER - ARGUMENTATION INTERFACE OPENS}}",
"{{PLACEHOLDER - TOPOGRAPHY LESSON PLAYS}}", "{{PLACEHOLDER - ARGUMENTATION TOPPO LESSON PLAYS}}", "{{PLACEHOLDER - SHIP SHAKES VIOLENTLY, DISTANT EXPLOSION}}",
"{{PLACEHOLDER - OPEN ARGUMENTATION INTERFACE}}","[[PLACEHOLDER - Argument]]","[[PLACEHOLDER - Skipping the facility because it isn't in the scene yet]]",
"[nosubtitle]", "<color=#35F>", "<color=#F53>", "*", '"', '"', "(brightly)", "(getting excited)", "…", "“", "”", "(muttering to herself)", "</color>",
"[[Placeholder - Character Customization]]",
"{{PLACEHOLDER- DANI MENU ACTIVATION ANIMATION}}",
"{{PLACEHOLDER - MENU OPENS AUTOMATICALLY}}",
"{{PLACEHOLDER - PLAYER CLOSES MENU}",
"{{PLACEHOLDER - ARGUMENTATION TOPPO LESSON PLAYS}}",
"{{PLACEHOLDER - TOPOGRAPHY LESSON PLAYS}}",
"{{PLACEHOLDER - ARGUMENTATION INTERFACE OPENS}}",
"[In ear]",
"{{PLACEHOLDER - CLOSE MAP}}",
"{{PLACEHOLDER - MAP OPENS AUTOMATICALLY}}",
"{{PLACEHOLDER - MAP OPENS}}",
"{{PLACEHOLDER - OPEN ARGUMENTATION INTERFACE}}",
"{{PLACEHOLDER - CLASSIFICATION EXERCISE AND FEEDBACK}}",
"{{PLACEHOLDER - TRANSITION TO BASE CAMP}}",
"[Placeholder - Character Customization]]"
]


replace_items = {"’":"'", "–":" ", "-":" ", "—":" ", "TK":"Tea Kay", "WAT247":"Watt 2 4 7", "Mission HydroSci": "Mission Hydro Sci", "Mission Hydrosci": "Mission Hydro Sci", "DANI": "Danny", "Argh!":"ArrGh"}
def updateText(userInput):
    output = userInput
    item = ""
    try:
        for item in remove_items:
            output = output.replace(item, '')
        for item in replace_items.keys():
            output = output.replace(item, replace_items[item])
    except Exception as e:
        print(f"error:{e}")
        print(f"item: {item}")
    return output

