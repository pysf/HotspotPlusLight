package e2e

// expectedThemes is the catalog of PLAN section 16 (copied from the plan, not from the code under test).
type expectedTheme struct {
	ID, Category, Title, Primary, Overlay string
	BackgroundBytes                       int
}

var expectedThemes = []expectedTheme{
	{"alpha-fall", "Alpha", "Fall", "rgb(118, 24, 60)", "rgba(118, 24, 60, 0.58)", 50298},
	{"alpha-space", "Alpha", "Space", "rgb(89, 61, 255)", "rgba(29, 22, 58, 0.64)", 49615},
	{"alpha-field", "Alpha", "Field", "rgb(255, 119, 56)", "rgba(120, 60, 30, 0.58)", 50730},
	{"restaurant-pasta", "Restaurant", "Pasta", "rgb(52, 119, 13)", "rgba(51, 55, 56, 0.5)", 49488},
	{"restaurant-pizza", "Restaurant", "Pizza", "rgb(166, 57, 35)", "rgba(116, 75, 66, 0.64)", 51394},
	{"restaurant-mushroomPizza", "Restaurant", "Mushroom Pizza", "rgb(221, 48, 18)", "rgba(0, 0, 0, 0.67)", 52172},
	{"restaurant-fastfood", "Restaurant", "Fastfood", "rgb(241, 126, 49)", "rgba(59, 45, 42, 0.4)", 51350},
	{"restaurant-traditional", "Restaurant", "Traditional", "rgb(48, 105, 84)", "rgba(20, 36, 30, 0.5)", 51718},
	{"cafe-breakfast", "Cafe", "Breakfast", "rgb(69, 99, 95)", "rgba(58, 67, 66, 0.58)", 51251},
	{"cafe-walnut", "Cafe", "Walnut", "rgb(175, 142, 122)", "rgba(0, 0, 0, 0.5)", 50712},
	{"cafe-sunflower", "Cafe", "Sunflower", "rgb(209, 102, 0)", "rgba(51, 15, 15, 0.5)", 54302},
	{"cafe-supper", "Cafe", "Supper", "rgb(100, 71, 173)", "rgba(57, 41, 41, 0.5)", 49843},
	{"cafe-lantern", "Cafe", "Lantern", "rgb(233, 149, 82)", "rgba(0, 0, 0, 0.5)", 50910},
	{"cafe-croisan", "Cafe", "Croisan", "rgb(3, 110, 186)", "rgba(65, 77, 86, 0.47)", 49692},
	{"cafe-caffeine", "Cafe", "Caffeine", "rgb(169, 48, 31)", "rgba(0, 0, 0, 0.5)", 54439},
	{"hotel-room", "Hotel", "Room", "rgb(77, 19, 41)", "rgba(71, 40, 53, 0.74)", 51550},
	{"hotel-beach", "Hotel", "Beach", "rgb(91, 149, 199)", "rgba(0, 0, 0, 0.4)", 49397},
	{"hotel-bell", "Hotel", "Bell", "rgb(176, 121, 25)", "rgba(0, 0, 0, 0.38)", 50567},
	{"hotel-calm", "Hotel", "Calm", "rgb(127, 74, 123)", "rgba(0, 0, 0, 0.38)", 46865},
	{"hotel-lobby", "Hotel", "Lobby", "rgb(54, 53, 71)", "rgba(0, 0, 0, 0.38)", 51602},
	{"hotel-luggage", "Hotel", "Luggage", "rgb(60, 156, 135)", "rgba(0, 0, 0, 0.5)", 55869},
	{"hotel-mountains", "Hotel", "Mountains", "rgb(233, 92, 37)", "rgba(0, 0, 0, 0.3)", 53083},
	{"hotel-reception", "Hotel", "Reception", "rgb(220, 20, 26)", "rgba(0, 0, 0, 0.38)", 51910},
	{"hotel-vacation", "Hotel", "Vacation", "rgb(70, 109, 121)", "rgba(0, 0, 0, 0.58)", 45898},
	{"university-library", "University", "Library", "rgb(15, 103, 200)", "rgba(13, 14, 16, 0.45)", 50373},
	{"university-drawing", "University", "Drawing", "rgb(80, 127, 98)", "rgba(0, 0, 0, 0.38)", 53579},
	{"university-sketch", "University", "Sketch", "rgb(244, 92, 36)", "rgba(0, 0, 0, 0.38)", 51256},
	{"university-fox", "University", "Fox", "rgb(254, 108, 91)", "rgba(0, 0, 0, 0.38)", 51415},
	{"university-notes", "University", "Notes", "rgb(175, 65, 65)", "rgba(0, 0, 0, 0.38)", 51748},
	{"university-graduation", "University", "Graduation", "rgb(99, 70, 173)", "rgba(0, 0, 0, 0.38)", 58054},
	{"university-study", "University", "Study", "rgb(0, 175, 181)", "rgba(0, 0, 0, 0.38)", 51871},
	{"market-alley", "Market", "Alley", "rgb(234, 95, 24)", "rgba(0, 0, 0, 0.4)", 56933},
	{"market-choice", "Market", "Choice", "rgb(1, 92, 111)", "rgba(0, 0, 0, 0.4)", 54917},
	{"market-autumn", "Market", "Autumn", "rgb(195, 94, 1)", "rgba(0, 0, 0, 0.4)", 50880},
	{"market-fruitBasket", "Market", "Fruit Basket", "rgb(207, 54, 14)", "rgba(0, 0, 0, 0.4)", 52671},
	{"market-size", "Market", "Size", "rgb(3, 142, 95)", "rgba(0, 0, 0, 0.4)", 51316},
	{"market-strawberries", "Market", "Strawberries", "rgb(163, 7, 1)", "rgba(0, 0, 0, 0.67)", 56173},
	{"market-veggies", "Market", "Veggies", "rgb(91, 149, 199)", "rgba(0, 0, 0, 0.4)", 51152},
}
