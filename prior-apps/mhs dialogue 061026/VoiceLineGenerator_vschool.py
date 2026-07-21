import csv
import UpdateText
import Entry
import os
import datetime
import time
from elevenlabs.client import ElevenLabs
from elevenlabs import play, save, stream, Voice, VoiceSettings
client = ElevenLabs(api_key="YOUR_ELEVENLABS_API_KEY")
def main():
    entries = readEntries('dialogue.csv')

    createFolders()
    entries = readEntries('dialogue.csv')
    x = 0
    startTime = datetime.datetime.now()
    for entry in entries:
        produceFile(entry)
        if(x % 100 == 0):
           print('Total time: ' + str((datetime.datetime.now() - startTime)))
        x += 1
    print('All done!')
    print(x)
    print('Total time: ' + str((datetime.datetime.now() - startTime)))

#Reads all entries from the given file, assuming the correct format
#Returns a list of Entry objects correlating to all valid entries in the document

#Parameter csvFile: String that contains the name of the csv that values will be read from.
def readEntries(csvFile):
    voices = readVoices('VoiceAssignments.csv')
    NPCVoices = voices[1]
    PlayerVoices = voices[0]
    entries = []
    f = open(csvFile, 'r', encoding="utf8")
    reader = csv.reader(f)
    lines = []
    for row in reader:
        if(len(row) > 0):
            if(row[0] == 'DialogueEntries'):
                break
    for x in range(2):
        next(reader)
    for row in reader:
        if(row[0] == 'OutgoingLinks'):
            break
        #if(row[5] == 'START'):
        #    continue
        entryTag = row[0]
        splitTag = entryTag.split('_')
        if(len(splitTag) > 3):
            name = ''
            for y in range(len(splitTag) - 2):
                name = name + splitTag[y+1]
                if(y < len(splitTag)-3):
                    name = name + ' ' 
        else:
            name = splitTag[1]
        try:
            text = row[5]
        except:
            text = ''
        print(text)
        if(name == 'Player'):
            for voice in PlayerVoices:
                newEntry = Entry.Entry(entryTag, voice[0], voice[1], text)
                entries.append(newEntry)
        else:
            
            newEntry = Entry.Entry(entryTag, NPCVoices[name][0], NPCVoices[name][1], text)
            entries.append(newEntry)
    f.close()
    return entries

#This function reads voices from a csv file assuming the correct format.
#It is returned with a list containing three elements:
#returnVal[0]: a list two element lists that contains player voice IDs and Names.
#returnVal[1]: a dictionary containing voice assignments for all NPCs. The NPC name is the key, the value is a two element list containing voiceID, voiceName
#returnVal[2]: a list of two element lists containing folder names for each individual voice entry, along with the VoiceID to be stored in the folder as a .TXT file.

#Parameter csvFile: A string containing the name of the csv that contains all voice assignments.
def readVoices(csvFile):
    voicesDict = {}
    playerVoices = []
    folders = []
    f = open(csvFile, 'r')
    reader = csv.reader(f)
    next(reader)
    for row in reader:
        if(row[0].lower() == 'player'):
            playerVoices.append([row[1], row[2]])
        else:
            voicesDict[row[0]] = [row[1], row[2]]
        name = row[0].replace(' ', '_')
        folders.append([name + '__' + row[2], row[1]])
    returnVal = [playerVoices, voicesDict, folders]
    f.close()
    return returnVal

#Generates an Eleven Labs audio file with the given passed Entry object
#If an audio file with the given tag and voice already exists, a new file is not produced.
#Return value is False if the file already exists, True if a new file was created.

#Parameter myEntry: an Entry object to be created, assuming a folder already exists for the given character-voice combination.
def produceFile(myEntry):
    dialogue_text = myEntry.getText()
    dialogue_text = dialogue_text.strip()
    if (dialogue_text == ''):
        print("Empty entry. Complete.")
        return False

    path = 'VoiceLines/' + myEntry.getName() + '__' + myEntry.getVoiceName() + '/' + myEntry.getTag() + '.mp3'
    if(not os.path.isfile(path)):
        print('Creating audio file ' + myEntry.getTag())
        audio = client.generate(
           text= myEntry.getText(),
           voice=myEntry.getVoiceID()
        )
        save(audio, path)
        print('Complete!')
        return True
    else:
        return False


#Generates folders for each NPC and player voice.
#The folders are stored with the naming convention: 'characterName__voiceName'
#If the folder already exists, the function will not attempt to create a new one.
def createFolders():
    entries = readVoices('VoiceAssignments.csv')
    folderNames = entries[2]
    if(not os.path.exists('VoiceLines')):
        os.mkdir('VoiceLines')
    for name in folderNames:
        if(not os.path.exists('VoiceLines/' + name[0])):
            os.mkdir('VoiceLines/' + name[0])
            
main()
