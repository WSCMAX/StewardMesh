package campusseed

import (
	"fmt"
	"strings"
	"sync"
)

// Campus identity names follow a Zipf–Mandelbrot rank-frequency curve, the
// usual demographic model for given and family names. Common names occupy the
// head of the curve and a long tail of less common names still appears, so a
// 21k-person import looks like a real campus instead of 100 looping pairs.
const campusNamePopulation = EmployeeCount + ActiveStudentCount + InactiveStudentCount

type campusName struct {
	First   string
	Last    string
	Display string
}

func campusEmployeeName(index int) campusName {
	return campusGeneratedName(index)
}

func campusStudentName(index int) campusName {
	return campusGeneratedName(EmployeeCount + index)
}

func campusGeneratedName(index int) campusName {
	campusNamesOnce.Do(buildCampusNames)
	if index < 0 || index >= len(campusNames) {
		return campusName{First: "Casey", Last: "Hall", Display: "Casey Hall"}
	}
	return campusNames[index]
}

func (n campusName) emailLocal(sequence, width int) string {
	return fmt.Sprintf("%s.%s%0*d", strings.ToLower(n.First), strings.ToLower(n.Last), width, sequence)
}

var (
	campusNamesOnce sync.Once
	campusNames     []campusName
	givenWeights    []int
	familyWeights   []int
)

func buildCampusNames() {
	givenWeights = nameWeights(len(campusGivenNames), 120, 15)
	familyWeights = nameWeights(len(campusFamilyNames), 250, 10)
	seats := apportion(givenWeights, campusNamePopulation)
	roster := make([]campusName, 0, campusNamePopulation)
	for firstIdx, count := range seats {
		if count == 0 {
			continue
		}
		first := campusGivenNames[firstIdx]
		lasts := expandFamilyNames(count, mix64(uint64(firstIdx+1)*0x9E3779B97F4A7C15))
		seen := make(map[string]int, count)
		for _, last := range lasts {
			occurrence := seen[last]
			seen[last] = occurrence + 1
			display := first + " " + last
			if occurrence > 0 {
				display = first + " " + campusMiddleNames[(occurrence-1)%len(campusMiddleNames)] + " " + last
			}
			roster = append(roster, campusName{First: first, Last: last, Display: display})
		}
	}
	if len(roster) != campusNamePopulation {
		panic(fmt.Sprintf("campus name roster is %d, want %d", len(roster), campusNamePopulation))
	}
	shuffleNames(roster, 0xA11C0DE5EED)
	campusNames = roster
}

func expandFamilyNames(count int, salt uint64) []string {
	seats := apportion(familyWeights, count)
	names := make([]string, 0, count)
	for index, seat := range seats {
		for i := 0; i < seat; i++ {
			names = append(names, campusFamilyNames[index])
		}
	}
	shuffleStrings(names, salt)
	return names
}

func nameWeights(count, scale, shift int) []int {
	weights := make([]int, count)
	for rank := 0; rank < count; rank++ {
		weight := scale / (rank + shift)
		if weight < 1 {
			weight = 1
		}
		weights[rank] = weight
	}
	return weights
}

func apportion(weights []int, seats int) []int {
	result := make([]int, len(weights))
	if len(weights) == 0 || seats <= 0 {
		return result
	}
	sum := 0
	for _, weight := range weights {
		sum += weight
	}
	if sum == 0 {
		return result
	}
	type remainder struct {
		index int
		frac  int
	}
	remainders := make([]remainder, len(weights))
	assigned := 0
	for index, weight := range weights {
		exact := weight * seats
		result[index] = exact / sum
		remainders[index] = remainder{index: index, frac: exact % sum}
		assigned += result[index]
	}
	for i := 0; i < len(remainders); i++ {
		best := i
		for j := i + 1; j < len(remainders); j++ {
			if remainders[j].frac > remainders[best].frac ||
				(remainders[j].frac == remainders[best].frac && remainders[j].index < remainders[best].index) {
				best = j
			}
		}
		remainders[i], remainders[best] = remainders[best], remainders[i]
	}
	for index := 0; assigned < seats; index++ {
		result[remainders[index%len(remainders)].index]++
		assigned++
	}
	return result
}

func shuffleNames(values []campusName, salt uint64) {
	rng := splitmix{state: salt}
	for i := len(values) - 1; i > 0; i-- {
		j := int(rng.next() % uint64(i+1))
		values[i], values[j] = values[j], values[i]
	}
}

func shuffleStrings(values []string, salt uint64) {
	rng := splitmix{state: salt}
	for i := len(values) - 1; i > 0; i-- {
		j := int(rng.next() % uint64(i+1))
		values[i], values[j] = values[j], values[i]
	}
}

type splitmix struct{ state uint64 }

func (s *splitmix) next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

var campusGivenNames = []string{
	"James", "Maria", "Michael", "Jennifer", "Robert", "Linda", "David", "Elizabeth", "William", "Barbara",
	"Richard", "Susan", "Joseph", "Jessica", "Thomas", "Sarah", "Charles", "Karen", "Christopher", "Nancy",
	"Daniel", "Lisa", "Matthew", "Betty", "Anthony", "Margaret", "Mark", "Sandra", "Donald", "Ashley",
	"Steven", "Kimberly", "Andrew", "Emily", "Paul", "Donna", "Joshua", "Michelle", "Kenneth", "Carol",
	"Kevin", "Amanda", "Brian", "Melissa", "George", "Deborah", "Timothy", "Stephanie", "Ronald", "Rebecca",
	"Edward", "Sharon", "Jason", "Laura", "Jeffrey", "Cynthia", "Ryan", "Kathleen", "Jacob", "Amy",
	"Gary", "Angela", "Nicholas", "Shirley", "Eric", "Anna", "Jonathan", "Brenda", "Stephen", "Pamela",
	"Larry", "Emma", "Justin", "Nicole", "Scott", "Helen", "Brandon", "Samantha", "Benjamin", "Katherine",
	"Samuel", "Christine", "Gregory", "Debra", "Alexander", "Rachel", "Patrick", "Carolyn", "Frank", "Janet",
	"Raymond", "Catherine", "Jack", "Dennis", "Heather", "Jerry", "Diane", "Tyler", "Julie", "Aaron",
	"Joyce", "Jose", "Victoria", "Adam", "Kelly", "Henry", "Christina", "Nathan", "Joan", "Peter",
	"Evelyn", "Zachary", "Judith", "Walter", "Megan", "Kyle", "Andrea", "Harold", "Cheryl", "Carl",
	"Hannah", "Jeremy", "Jacqueline", "Keith", "Martha", "Roger", "Gloria", "Gerald", "Teresa", "Ethan",
	"Sara", "Arthur", "Janice", "Terry", "Marie", "Christian", "Julia", "Sean", "Grace", "Lawrence",
	"Judy", "Austin", "Theresa", "Joe", "Madison", "Noah", "Beverly", "Jesse", "Denise", "Albert",
	"Marilyn", "Bryan", "Amber", "Billy", "Danielle", "Bruce", "Brittany", "Willie", "Diana", "Gabriel",
	"Abigail", "Jane", "Logan", "Lori", "Alan", "Olivia", "Juan", "Frances", "Wayne", "Kayla",
	"Roy", "Alexis", "Ralph", "Lorraine", "Randy", "Alice", "Eugene", "Tiffany", "Vincent", "Kathy",
	"Russell", "Rose", "Louis", "Sophia", "Philip", "Isabella", "Bobby", "Mia", "Johnny", "Camila",
	"Bradley", "Sofia", "Luis", "Valentina", "Carlos", "Aaliyah", "Victor", "Destiny", "Martin", "Imani",
	"Ernest", "Keisha", "Craig", "Fatima", "Stanley", "Aisha", "Leonard", "Amina", "Derek", "Priya",
	"Caleb", "Ananya", "Shawn", "Mei", "Travis", "Yuki", "Ian", "Hiro", "Julian", "Min",
	"Omar", "Kenji", "Diego", "Wei", "Miguel", "Hassan", "Andre", "Yusuf", "Malik", "Jamal",
}

var campusFamilyNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez",
	"Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin",
	"Lee", "Perez", "Thompson", "White", "Harris", "Sanchez", "Clark", "Ramirez", "Lewis", "Robinson",
	"Walker", "Young", "Allen", "King", "Wright", "Scott", "Torres", "Nguyen", "Hill", "Flores",
	"Green", "Adams", "Nelson", "Baker", "Hall", "Rivera", "Campbell", "Mitchell", "Carter", "Roberts",
	"Gomez", "Phillips", "Evans", "Turner", "Diaz", "Parker", "Cruz", "Edwards", "Collins", "Reyes",
	"Stewart", "Morris", "Morales", "Murphy", "Cook", "Rogers", "Gutierrez", "Ortiz", "Morgan", "Cooper",
	"Peterson", "Bailey", "Reed", "Kelly", "Howard", "Ramos", "Kim", "Cox", "Ward", "Richardson",
	"Watson", "Brooks", "Chavez", "Wood", "James", "Bennett", "Gray", "Mendoza", "Ruiz", "Hughes",
	"Price", "Alvarez", "Castillo", "Sanders", "Patel", "Myers", "Long", "Ross", "Foster", "Jimenez",
	"Powell", "Jenkins", "Perry", "Russell", "Sullivan", "Bell", "Coleman", "Butler", "Henderson", "Barnes",
	"Gonzales", "Fisher", "Vasquez", "Simmons", "Romero", "Jordan", "Patterson", "Alexander", "Hamilton", "Graham",
	"Reynolds", "Griffin", "Wallace", "Moreno", "West", "Cole", "Hayes", "Bryant", "Herrera", "Gibson",
	"Ellis", "Tran", "Medina", "Aguilar", "Stevens", "Murray", "Ford", "Castro", "Marshall", "Owen",
	"Harrison", "Fernandez", "McDonald", "Woods", "Washington", "Kennedy", "Wells", "Vargas", "Henry", "Chen",
	"Freeman", "Webb", "Tucker", "Guzman", "Burns", "Crawford", "Olson", "Simpson", "Porter", "Hunter",
	"Gordon", "Mendez", "Silva", "Shaw", "Snyder", "Mason", "Dixon", "Munoz", "Hunt", "Hicks",
	"Holmes", "Palmer", "Wagner", "Black", "Robertson", "Boyd", "Rose", "Stone", "Salazar", "Fox",
	"Warren", "Mills", "Meyer", "Rice", "Schmidt", "Garza", "Daniels", "Ferguson", "Nichols", "Stephens",
	"Soto", "Weaver", "Ryan", "Gardner", "Payne", "Grant", "Dunn", "Kelley", "Spencer", "Hawkins",
	"Arnold", "Pierce", "Vazquez", "Hansen", "Peters", "Santos", "Hart", "Bradley", "Knight", "Elliott",
	"Cunningham", "Duncan", "Armstrong", "Hudson", "Carroll", "Lane", "Riley", "Andrews", "Alvarado", "Ray",
	"Delgado", "Berry", "Perkins", "Hoffman", "Johnston", "Matthews", "Pena", "Richards", "Contreras", "Willis",
	"Carpenter", "Lawrence", "Sandoval", "Guerrero", "George", "Chapman", "Rios", "Estrada", "Ortega", "Watkins",
	"Greene", "Nunez", "Wheeler", "Valdez", "Harper", "Burke", "Larson", "Bishop", "Singh", "Shah",
}

var campusMiddleNames = []string{
	"Marie", "Ann", "Lee", "James", "Michael", "Grace", "Rose", "Lynn", "Joseph", "Elizabeth",
	"Alexander", "Claire", "David", "Jane", "Thomas", "Kate", "Andrew", "Nicole", "Daniel", "May",
	"Patrick", "Hope", "Edward", "Faith", "William", "Joy", "Robert", "Pearl", "Charles", "Quinn",
	"Henry", "Blake", "Samuel", "Reese", "Owen", "Skye", "Jade", "Cole", "Eden", "Ruth",
}
