package ui

// These are the normal emoji, in groups, for the emoji picker. Each has a few
// words to search by.

type stdEmoji struct{ char, words string }

type emojiGroup struct {
	title string
	list  []stdEmoji
}

var emojiGroups = []emojiGroup{
	{"SMILEYS", []stdEmoji{
		{"😀", "grin smile happy"}, {"😃", "smile happy"}, {"😄", "smile laugh"}, {"😁", "grin beam"},
		{"😆", "laugh squint"}, {"😅", "sweat smile"}, {"🤣", "rofl laugh floor"}, {"😂", "joy tears laugh"},
		{"🙂", "slight smile"}, {"🙃", "upside down"}, {"😉", "wink"}, {"😊", "blush smile"},
		{"😇", "angel halo"}, {"😍", "heart eyes love"},
		{"😘", "kiss"}, {"😋", "yum tasty"}, {"😛", "tongue"}, {"😜", "wink tongue"},
		{"🤗", "hug"},
		{"🤔", "think thinking"}, {"🤐", "zip mouth"}, {"😐", "neutral"},
		{"😑", "expressionless"}, {"😶", "no mouth"}, {"😏", "smirk"}, {"😒", "unamused"},
		{"🙄", "eye roll"}, {"😬", "grimace"}, {"😌", "relieved"}, {"😔", "pensive sad"},
		{"😪", "sleepy"}, {"😴", "sleep zzz"}, {"😷", "mask sick"}, {"🤒", "fever sick"},
		{"🤕", "hurt bandage"}, {"🤢", "nausea sick"},
		{"😵", "dizzy"},
		{"😎", "cool sunglasses"}, {"🤓", "nerd"}, {"😕", "confused"},
		{"😟", "worried"}, {"🙁", "frown"}, {"😮", "open mouth wow"}, {"😲", "astonished"},
		{"😳", "flushed"}, {"😢", "cry sad"}, {"😭", "sob cry"},
		{"😱", "scream"}, {"😖", "confounded"}, {"😞", "disappointed"}, {"😓", "sweat"},
		{"😩", "weary"}, {"😫", "tired"}, {"😤", "triumph huff"},
		{"😡", "angry rage"}, {"😠", "angry"}, {"😈", "devil smile"},
		{"💀", "skull dead"}, {"💩", "poop"}, {"🤡", "clown"}, {"👻", "ghost"},
		{"👽", "alien"}, {"🤖", "robot"}, {"😺", "cat smile"}, {"😻", "cat heart eyes"},
	}},
	{"PEOPLE", []stdEmoji{
		{"👋", "wave hello"}, {"🤚", "raised back hand"}, {"✋", "hand stop"}, {"🖖", "vulcan"},
		{"👌", "ok"}, {"✌️", "victory peace"}, {"🤞", "fingers crossed luck"},
		{"🤘", "rock horns"}, {"🤙", "call me"}, {"👈", "point left"},
		{"👉", "point right"}, {"👆", "point up"}, {"👇", "point down"}, {"☝️", "index up"},
		{"👍", "thumbs up like yes"}, {"👎", "thumbs down dislike no"}, {"✊", "fist"}, {"👊", "punch"},
		{"👏", "clap"}, {"🙌", "raise hands"}, {"👐", "open hands"},
		{"🤝", "handshake deal"}, {"🙏", "pray please thanks namaste"}, {"✍️", "write"}, {"💪", "muscle strong"},
		{"👀", "eyes look"}, {"👁️", "eye"},
		{"👶", "baby"}, {"👩", "woman"},
		{"👨", "man"}, {"👮", "police"},
		{"🎓", "graduate student"},
		{"🙋", "raise hand"}, {"🤷", "shrug"}, {"🤦", "facepalm"}, {"🙇", "bow"},
	}},
	{"NATURE", []stdEmoji{
		{"🐶", "dog puppy"}, {"🐱", "cat kitten"}, {"🐭", "mouse"}, {"🐰", "rabbit bunny"},
		{"🦊", "fox"}, {"🐻", "bear"}, {"🐼", "panda"}, {"🐨", "koala"},
		{"🐯", "tiger"}, {"🦁", "lion"}, {"🐮", "cow"}, {"🐷", "pig"},
		{"🐸", "frog"}, {"🐵", "monkey"}, {"🐔", "chicken"}, {"🐧", "penguin"},
		{"🐦", "bird"}, {"🦆", "duck"}, {"🦉", "owl"}, {"🐝", "bee"},
		{"🦋", "butterfly"}, {"🐢", "turtle"}, {"🐍", "snake"}, {"🐙", "octopus"},
		{"🐬", "dolphin"}, {"🐳", "whale"}, {"🐘", "elephant"},
		{"🌸", "blossom flower"}, {"🌹", "rose"}, {"🌻", "sunflower"}, {"🌷", "tulip"},
		{"🌱", "seedling plant"}, {"🌳", "tree"}, {"🌴", "palm"}, {"🍀", "clover luck"},
		{"🍁", "maple leaf"}, {"🌙", "moon"}, {"⭐", "star"}, {"🌟", "glowing star"},
		{"✨", "sparkles"}, {"☀️", "sun"}, {"⛅", "cloud sun"}, {"🌧️", "rain"},
		{"⛈️", "storm"}, {"❄️", "snow"}, {"🌈", "rainbow"}, {"🔥", "fire lit"},
		{"💧", "drop water"}, {"🌊", "wave sea"}, {"🌍", "earth world"},
	}},
	{"FOOD", []stdEmoji{
		{"🍎", "apple"}, {"🍊", "orange"}, {"🍋", "lemon"}, {"🍌", "banana"},
		{"🍉", "watermelon"}, {"🍇", "grapes"}, {"🍓", "strawberry"},
		{"🍍", "pineapple"}, {"🥑", "avocado"}, {"🍅", "tomato"},
		{"🌶️", "chilli pepper"}, {"🌽", "corn"}, {"🥕", "carrot"}, {"🥔", "potato"},
		{"🍞", "bread"}, {"🧀", "cheese"}, {"🍳", "egg cooking"}, {"🍔", "burger"},
		{"🍟", "fries"}, {"🍕", "pizza"}, {"🌮", "taco"}, {"🍛", "curry rice"},
		{"🍚", "rice"}, {"🍜", "noodles"}, {"🍣", "sushi"},
		{"🍪", "cookie"}, {"🎂", "cake birthday"}, {"🍰", "cake slice"}, {"🍫", "chocolate"},
		{"🍬", "candy sweet"}, {"🍩", "doughnut"}, {"🍦", "ice cream"}, {"🍿", "popcorn"},
		{"☕", "coffee tea"}, {"🍵", "tea"},
	}},
	{"ACTIVITIES", []stdEmoji{
		{"⚽", "football soccer"}, {"🏏", "cricket"}, {"🏀", "basketball"}, {"🏈", "american football"},
		{"⚾", "baseball"}, {"🎾", "tennis"}, {"🏸", "badminton"}, {"🏓", "table tennis ping pong"},
		{"🏐", "volleyball"}, {"🥊", "boxing"}, {"🏆", "trophy win"}, {"🥇", "gold medal first"},
		{"🥈", "silver medal second"}, {"🥉", "bronze medal third"}, {"🎯", "target bullseye"}, {"🎮", "game controller"},
		{"🕹️", "joystick"}, {"🎲", "dice"}, {"♟️", "chess"},
		{"🎨", "art palette paint"}, {"🖌️", "brush paint"}, {"🎭", "theatre"}, {"🎤", "microphone sing"},
		{"🎧", "headphones music"}, {"🎸", "guitar"}, {"🎹", "piano"}, {"🥁", "drum"},
		{"🎬", "film movie"}, {"📸", "camera photo"}, {"🎉", "party popper tada"}, {"🎊", "confetti"},
		{"🎁", "gift present"}, {"🎈", "balloon"}, {"🎆", "fireworks"},
	}},
	{"TRAVEL", []stdEmoji{
		{"🚗", "car"}, {"🚕", "taxi"}, {"🚌", "bus"},
		{"🚆", "train"}, {"🚇", "metro"}, {"🚲", "bicycle"}, {"🛵", "scooter"},
		{"✈️", "plane flight"}, {"🚀", "rocket launch"}, {"🚢", "ship"},
		{"⛵", "boat"}, {"🏠", "house home"}, {"🏢", "office building"}, {"🏫", "school"},
		{"🏥", "hospital"}, {"🏰", "castle"}, {"🕌", "mosque"},
		{"⛪", "church"}, {"🗻", "mountain"}, {"🏖️", "beach"}, {"🏕️", "camping"},
		{"🌆", "city"}, {"🌉", "bridge night"}, {"🗺️", "map"},
	}},
	{"OBJECTS", []stdEmoji{
		{"💻", "laptop computer"}, {"🖥️", "desktop computer"}, {"⌨️", "keyboard"}, {"🖱️", "mouse"},
		{"📱", "phone"}, {"☎️", "telephone"}, {"🔋", "battery"}, {"🔌", "plug"},
		{"💡", "idea bulb"}, {"🔦", "torch"}, {"📷", "camera"}, {"📺", "tv"},
		{"⏰", "alarm clock"}, {"⌛", "hourglass"}, {"📅", "calendar date"}, {"📌", "pin"},
		{"📎", "paperclip"}, {"✏️", "pencil"}, {"🖊️", "pen"}, {"📝", "memo note"},
		{"📚", "books"}, {"📖", "book read"}, {"🔒", "lock"}, {"🔑", "key"},
		{"🔨", "hammer"}, {"🛠️", "tools"}, {"⚙️", "gear settings"},
		{"💰", "money bag"}, {"💳", "card"}, {"💎", "gem diamond"},
		{"📦", "package box"}, {"✉️", "envelope mail"}, {"📣", "megaphone"}, {"🔔", "bell"},
	}},
	{"SYMBOLS", []stdEmoji{
		{"❤️", "heart love red"}, {"💛", "yellow heart"}, {"💚", "green heart"},
		{"💙", "blue heart"}, {"💜", "purple heart"}, {"🖤", "black heart"},
		{"💔", "broken heart"}, {"💕", "two hearts"}, {"💯", "hundred"}, {"💢", "anger"},
		{"💥", "boom"}, {"💫", "dizzy star"}, {"💬", "speech"}, {"💤", "zzz sleep"},
		{"✅", "check done yes"}, {"☑️", "ballot check"}, {"✔️", "tick"}, {"❌", "cross no"},
		{"❎", "cross button"}, {"➕", "plus"}, {"➖", "minus"}, {"❓", "question"},
		{"❗", "exclamation"}, {"⚠️", "warning"}, {"🚫", "forbidden no"}, {"⛔", "no entry"},
		{"🔴", "red circle"},
		{"🔵", "blue circle"}, {"⚫", "black circle"}, {"⚪", "white circle"},
		{"⬆️", "up arrow"}, {"⬇️", "down arrow"}, {"⬅️", "left arrow"}, {"➡️", "right arrow"},
		{"🔁", "repeat"}, {"🆕", "new"}, {"🆗", "ok button"}, {"🆒", "cool button"},
		{"🏳️", "white flag"}, {"🏁", "chequered flag finish"}, {"🚩", "red flag"},
	}},
}

// newerEmoji are popular emoji from 2017 on. They are offered only when
// Noto Color Emoji is in use; Fyne's own emoji font lacks them.
var newerEmoji = emojiGroup{"NEWER", []stdEmoji{
	{"🥰", "smiling hearts love"}, {"🤩", "star struck"}, {"🥳", "party celebrate"}, {"🥺", "pleading puppy eyes"},
	{"🥲", "smile tear"}, {"🥹", "holding back tears"}, {"🫠", "melting"}, {"🤪", "zany crazy"},
	{"🤯", "mind blown"}, {"🤭", "hand over mouth giggle"}, {"🤫", "shush quiet"}, {"🫣", "peek"},
	{"🫢", "gasp"}, {"🫡", "salute"}, {"🧐", "monocle"}, {"🥱", "yawn tired"},
	{"🥵", "hot"}, {"🥶", "cold freezing"}, {"🥴", "woozy"}, {"🤠", "cowboy"},
	{"🫶", "heart hands love"}, {"🤌", "pinched fingers"}, {"🤞", "fingers crossed luck"}, {"🫰", "finger heart"},
	{"🧡", "orange heart"}, {"🤎", "brown heart"}, {"🤍", "white heart"}, {"🩷", "pink heart"},
	{"🦩", "flamingo"}, {"🦥", "sloth"}, {"🪿", "goose"}, {"🫎", "moose"},
	{"🧁", "cupcake"}, {"🧋", "bubble tea"}, {"🪅", "pinata"}, {"🎮", "game controller"},
}}

// pickerGroups are the emoji the picker offers.
func pickerGroups() []emojiGroup {
	if fyneEmojiFont != nil && fyneEmojiFont.StaticName == emojiFontFile {
		return append([]emojiGroup{newerEmoji}, emojiGroups...)
	}
	return emojiGroups
}
