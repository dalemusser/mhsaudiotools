import UpdateText
class Entry():
    def __init__(self, __entrytag, __voiceID, __voiceName, __dialogueText):
        self.__entrytag = __entrytag
        self.__voiceID = __voiceID
        self.__voiceName = __voiceName
        self.__dialogueText = UpdateText.updateText(__dialogueText)
    def getTag(self):
        return self.__entrytag
    def getVoiceID(self):
        return self.__voiceID
    def getVoiceName(self):
        return self.__voiceName
    def getText(self):
        return self.__dialogueText
    def getName(self):
        splitTag = self.__entrytag.split('_')
        if(len(splitTag) > 3):
            name = ''
            for y in range(len(splitTag) - 2):
                name = name + splitTag[y+1]
                if(y < len(splitTag)-3):
                    name = name + '_'
            return(name)
        else:
            return splitTag[1]
        
