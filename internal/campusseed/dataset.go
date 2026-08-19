package campusseed

import "fmt"

const (
	SiteName                = "[Campus Demo] Riverside Community College"
	SiteCity                = "Riverside"
	SiteRegion              = "CA"
	ActorID                 = "system:campus-demo"
	SourceSystemID          = "campus-demo-v3"
	EmployeeCount           = 1050
	ActiveStudentCount      = 5000
	InactiveStudentCount    = 15000
	VisualArtsStudentCount  = 480
	StudentProvider         = "campus-demo-students"
	StudioArtsStudentTag    = "Studio Arts Student"
	GraphicDesignStudentTag = "Graphic Design Student"
	CredentialsFile         = "tmp/campus-test-users.env"
)

type buildingDef struct {
	Slug string
	Name string
}

type departmentDef struct {
	Slug         string
	Name         string
	School       string
	BuildingSlug string
	Headcount    int
}

type officeDef struct {
	Slug           string
	Name           string
	BuildingSlug   string
	RoomNumber     string
	DepartmentSlug string
	DeskCount      int
}

type shopDef struct {
	Slug               string
	Name               string
	BuildingSlug       string
	RoomNumber         string
	DepartmentSlug     string
	SharedDesktopCount int
}

type labDef struct {
	Slug           string
	Name           string
	BuildingSlug   string
	RoomNumber     string
	MachineCount   int
	ModelSlug      string
	SoftwareNotes  string
	DepartmentSlug string
}

type modelDef struct {
	Slug             string
	Manufacturer     string
	Name             string
	ModelNumber      string
	Kind             string
	UnitCostMinor    int64
	CriticalityScore int
	Specifications   map[string]string
}

type workforceLaptopDistribution struct {
	ModelSlug string
	Count     int
}

// campusWorkforceLaptops is the faculty/staff laptop fleet mix used by seeding and lifecycle planning.
var campusWorkforceLaptops = []workforceLaptopDistribution{
	{ModelSlug: "dell-latitude-7440", Count: 320},
	{ModelSlug: "dell-latitude-5540", Count: 200},
	{ModelSlug: "lenovo-thinkpad-t14-gen3", Count: 50},
	{ModelSlug: "lenovo-thinkpad-t14", Count: 80},
	{ModelSlug: "hp-elitebook-840", Count: 50},
}

var campusBuildings = []buildingDef{
	{Slug: "administration", Name: "Administration Building"},
	{Slug: "student-center", Name: "Student Center"},
	{Slug: "services", Name: "Services Building"},
	{Slug: "library", Name: "Library & Learning Commons"},
	{Slug: "education", Name: "Education Building"},
	{Slug: "sciences", Name: "Sciences Building"},
	{Slug: "math-history", Name: "Math & History Hall"},
	{Slug: "residence-hall", Name: "Riverside Residence Hall"},
}

var campusDepartments = []departmentDef{
	// Administration Building — enrollment, finance, and executive staff.
	{Slug: "financial-aid", Name: "Financial Aid", School: "Student Services", BuildingSlug: "administration", Headcount: 45},
	{Slug: "student-records", Name: "Student Records & Registration", School: "Student Services", BuildingSlug: "administration", Headcount: 42},
	{Slug: "human-resources", Name: "Human Resources", School: "Administrative", BuildingSlug: "administration", Headcount: 38},
	{Slug: "accounting", Name: "Accounting & Finance", School: "Administrative", BuildingSlug: "administration", Headcount: 34},
	{Slug: "executive-affairs", Name: "Executive & Board Affairs", School: "Administrative", BuildingSlug: "administration", Headcount: 18},

	// Student Center — student-facing services outside the registrar suite.
	{Slug: "student-affairs", Name: "Student Affairs", School: "Student Services", BuildingSlug: "student-center", Headcount: 36},
	{Slug: "campus-life", Name: "Campus Life & Activities", School: "Student Services", BuildingSlug: "student-center", Headcount: 22},

	// Services Building — facilities, IT, and security operations.
	{Slug: "facilities", Name: "Facilities Management", School: "Administrative", BuildingSlug: "services", Headcount: 20},
	{Slug: "custodial", Name: "Custodial Services", School: "Administrative", BuildingSlug: "services", Headcount: 32},
	{Slug: "it-services", Name: "IT Services", School: "Administrative", BuildingSlug: "services", Headcount: 58},
	{Slug: "campus-security", Name: "Campus Security", School: "Administrative", BuildingSlug: "services", Headcount: 44},

	// Library.
	{Slug: "library-services", Name: "Library Services", School: "Academic Support", BuildingSlug: "library", Headcount: 32},

	// Education Building — professional programs, arts, and computing.
	{Slug: "school-education", Name: "School of Education", School: "Academic", BuildingSlug: "education", Headcount: 78},
	{Slug: "school-business", Name: "School of Business & Technology", School: "Academic", BuildingSlug: "education", Headcount: 62},
	{Slug: "computer-science", Name: "Computer Science", School: "Business & Technology", BuildingSlug: "education", Headcount: 68},
	{Slug: "cybersecurity", Name: "Cybersecurity", School: "Business & Technology", BuildingSlug: "education", Headcount: 34},
	{Slug: "studio-arts", Name: "Studio Arts", School: "Arts & Humanities", BuildingSlug: "education", Headcount: 40},
	{Slug: "graphic-design", Name: "Graphic Design", School: "Arts & Humanities", BuildingSlug: "education", Headcount: 44},
	{Slug: "music", Name: "Music & Performing Arts", School: "Arts & Humanities", BuildingSlug: "education", Headcount: 36},

	// Sciences Building — STEM labs and clinical programs.
	{Slug: "biology", Name: "Biology", School: "Science & Engineering", BuildingSlug: "sciences", Headcount: 46},
	{Slug: "chemistry", Name: "Chemistry", School: "Science & Engineering", BuildingSlug: "sciences", Headcount: 40},
	{Slug: "physics", Name: "Physics", School: "Science & Engineering", BuildingSlug: "sciences", Headcount: 34},
	{Slug: "engineering", Name: "Engineering", School: "Science & Engineering", BuildingSlug: "sciences", Headcount: 52},
	{Slug: "architecture", Name: "Architecture", School: "Science & Engineering", BuildingSlug: "sciences", Headcount: 30},
	{Slug: "nursing", Name: "Nursing", School: "Health Sciences", BuildingSlug: "sciences", Headcount: 48},

	// Math & History Hall — mathematics and humanities grouped by wing.
	{Slug: "mathematics", Name: "Mathematics", School: "Arts & Sciences", BuildingSlug: "math-history", Headcount: 64},
	{Slug: "history", Name: "History", School: "Arts & Humanities", BuildingSlug: "math-history", Headcount: 56},
	{Slug: "english", Name: "English", School: "Arts & Humanities", BuildingSlug: "math-history", Headcount: 50},
	{Slug: "sociology", Name: "Sociology", School: "Arts & Humanities", BuildingSlug: "math-history", Headcount: 38},
	{Slug: "philosophy", Name: "Philosophy", School: "Arts & Humanities", BuildingSlug: "math-history", Headcount: 24},
}

var campusOffices = []officeDef{
	// Administration Building offices.
	{Slug: "admin-financial-aid", Name: "Financial Aid Office Suite", BuildingSlug: "administration", RoomNumber: "ADM-110", DepartmentSlug: "financial-aid", DeskCount: 18},
	{Slug: "admin-registrar", Name: "Registrar & Records Suite", BuildingSlug: "administration", RoomNumber: "ADM-120", DepartmentSlug: "student-records", DeskCount: 16},
	{Slug: "admin-hr", Name: "Human Resources Office Suite", BuildingSlug: "administration", RoomNumber: "ADM-210", DepartmentSlug: "human-resources", DeskCount: 14},
	{Slug: "admin-finance", Name: "Finance & Accounting Offices", BuildingSlug: "administration", RoomNumber: "ADM-220", DepartmentSlug: "accounting", DeskCount: 12},
	{Slug: "admin-executive", Name: "Executive Offices", BuildingSlug: "administration", RoomNumber: "ADM-300", DepartmentSlug: "executive-affairs", DeskCount: 10},

	// Student Center offices.
	{Slug: "sc-student-affairs", Name: "Student Affairs Offices", BuildingSlug: "student-center", RoomNumber: "SC-101", DepartmentSlug: "student-affairs", DeskCount: 14},
	{Slug: "sc-campus-life", Name: "Campus Life Offices", BuildingSlug: "student-center", RoomNumber: "SC-115", DepartmentSlug: "campus-life", DeskCount: 10},

	// Services Building offices.
	{Slug: "svc-it", Name: "IT Services Operations Center", BuildingSlug: "services", RoomNumber: "SVC-110", DepartmentSlug: "it-services", DeskCount: 20},
	{Slug: "svc-facilities", Name: "Facilities Dispatch & Planning", BuildingSlug: "services", RoomNumber: "SVC-120", DepartmentSlug: "facilities", DeskCount: 16},
	{Slug: "svc-security", Name: "Campus Security Office", BuildingSlug: "services", RoomNumber: "SVC-130", DepartmentSlug: "campus-security", DeskCount: 12},
	{Slug: "svc-custodial", Name: "Custodial Supervisors Office", BuildingSlug: "services", RoomNumber: "SVC-140", DepartmentSlug: "custodial", DeskCount: 8},

	// Library offices.
	{Slug: "lib-staff", Name: "Library Staff Offices", BuildingSlug: "library", RoomNumber: "LIB-210", DepartmentSlug: "library-services", DeskCount: 14},

	// Education Building faculty offices.
	{Slug: "edu-education-offices", Name: "Education Faculty Offices", BuildingSlug: "education", RoomNumber: "ED-110", DepartmentSlug: "school-education", DeskCount: 18},
	{Slug: "edu-business-offices", Name: "Business Faculty Offices", BuildingSlug: "education", RoomNumber: "ED-120", DepartmentSlug: "school-business", DeskCount: 16},
	{Slug: "edu-cs-offices", Name: "Computer Science Faculty Offices", BuildingSlug: "education", RoomNumber: "ED-210", DepartmentSlug: "computer-science", DeskCount: 20},
	{Slug: "edu-cyber-offices", Name: "Cybersecurity Program Offices", BuildingSlug: "education", RoomNumber: "ED-215", DepartmentSlug: "cybersecurity", DeskCount: 10},
	{Slug: "edu-arts-offices", Name: "Studio & Design Faculty Offices", BuildingSlug: "education", RoomNumber: "ED-310", DepartmentSlug: "studio-arts", DeskCount: 14},
	{Slug: "edu-design-offices", Name: "Graphic Design Faculty Offices", BuildingSlug: "education", RoomNumber: "ED-320", DepartmentSlug: "graphic-design", DeskCount: 12},
	{Slug: "edu-music-offices", Name: "Music Faculty Offices", BuildingSlug: "education", RoomNumber: "ED-315", DepartmentSlug: "music", DeskCount: 10},

	// Sciences Building faculty and program offices.
	{Slug: "sci-deans", Name: "Sciences Dean Suite", BuildingSlug: "sciences", RoomNumber: "SCI-110", DepartmentSlug: "biology", DeskCount: 8},
	{Slug: "sci-engineering-offices", Name: "Engineering Faculty Offices", BuildingSlug: "sciences", RoomNumber: "SCI-210", DepartmentSlug: "engineering", DeskCount: 16},
	{Slug: "sci-nursing-offices", Name: "Nursing Program Offices", BuildingSlug: "sciences", RoomNumber: "SCI-220", DepartmentSlug: "nursing", DeskCount: 14},
	{Slug: "sci-lab-coordinators", Name: "Sciences Lab Coordinator Offices", BuildingSlug: "sciences", RoomNumber: "SCI-230", DepartmentSlug: "chemistry", DeskCount: 12},
	{Slug: "sci-physics-offices", Name: "Physics Faculty Offices", BuildingSlug: "sciences", RoomNumber: "SCI-240", DepartmentSlug: "physics", DeskCount: 10},
	{Slug: "sci-arch-offices", Name: "Architecture Faculty Offices", BuildingSlug: "sciences", RoomNumber: "SCI-245", DepartmentSlug: "architecture", DeskCount: 10},

	// Math & History Hall faculty offices.
	{Slug: "mh-math-offices", Name: "Mathematics Faculty Offices", BuildingSlug: "math-history", RoomNumber: "MH-110", DepartmentSlug: "mathematics", DeskCount: 18},
	{Slug: "mh-history-offices", Name: "History Faculty Offices", BuildingSlug: "math-history", RoomNumber: "MH-120", DepartmentSlug: "history", DeskCount: 14},
	{Slug: "mh-humanities-offices", Name: "Humanities Faculty Offices", BuildingSlug: "math-history", RoomNumber: "MH-210", DepartmentSlug: "english", DeskCount: 16},
	{Slug: "mh-social-sciences", Name: "Social Sciences Offices", BuildingSlug: "math-history", RoomNumber: "MH-220", DepartmentSlug: "sociology", DeskCount: 12},
	{Slug: "mh-philosophy-offices", Name: "Philosophy Faculty Offices", BuildingSlug: "math-history", RoomNumber: "MH-225", DepartmentSlug: "philosophy", DeskCount: 8},
}

var campusShops = []shopDef{
	{Slug: "paint-shop", Name: "Paint Shop", BuildingSlug: "services", RoomNumber: "SVC-210", DepartmentSlug: "custodial", SharedDesktopCount: 8},
	{Slug: "greens-shop", Name: "Grounds & Greens Shop", BuildingSlug: "services", RoomNumber: "SVC-220", DepartmentSlug: "custodial", SharedDesktopCount: 6},
}

var campusModels = []modelDef{
	{
		Slug: "imac-m3", Manufacturer: "Apple", Name: "iMac 24-inch M3", ModelNumber: "MQRC3LL/A", Kind: "desktop",
		UnitCostMinor: 149900, Specifications: map[string]string{"cpu": "Apple M3 8-core", "memory": "16 GB", "storage": "512 GB SSD", "display": "24-inch 4.5K Retina"},
	},
	{
		Slug: "mac-studio-m2", Manufacturer: "Apple", Name: "Mac Studio M2 Max", ModelNumber: "MQH73LL/A", Kind: "desktop",
		UnitCostMinor: 199900, Specifications: map[string]string{"cpu": "Apple M2 Max 12-core", "memory": "32 GB", "storage": "1 TB SSD", "gpu": "30-core GPU"},
	},
	{
		Slug: "macbook-pro-m3", Manufacturer: "Apple", Name: "MacBook Pro 14-inch M3 Pro", ModelNumber: "MRX33LL/A", Kind: "laptop",
		UnitCostMinor: 199900, Specifications: map[string]string{"cpu": "Apple M3 Pro 11-core", "memory": "18 GB", "storage": "512 GB SSD", "display": "14-inch Liquid Retina XDR"},
	},
	{
		Slug: "dell-precision-7865", Manufacturer: "Dell", Name: "Precision 7865 Tower", ModelNumber: "7865-WS", Kind: "desktop",
		UnitCostMinor: 289900, CriticalityScore: 5,
		Specifications: map[string]string{"cpu": "AMD Ryzen Threadripper PRO 5955WX", "memory": "64 GB", "storage": "1 TB NVMe", "gpu": "NVIDIA RTX A4000 16 GB"},
	},
	{
		Slug: "dell-optiplex-7020", Manufacturer: "Dell", Name: "OptiPlex 7020 SFF", ModelNumber: "7020-SFF", Kind: "desktop",
		UnitCostMinor: 98900, CriticalityScore: 3,
		Specifications: map[string]string{"cpu": "Intel Core i5-14500", "memory": "16 GB", "storage": "512 GB SSD", "formFactor": "Small Form Factor"},
	},
	{
		Slug: "dell-latitude-5540", Manufacturer: "Dell", Name: "Latitude 5540", ModelNumber: "5540-STD", Kind: "laptop",
		UnitCostMinor: 124900, CriticalityScore: 4,
		Specifications: map[string]string{"cpu": "Intel Core i5-1345U", "memory": "16 GB", "storage": "512 GB SSD", "display": "15.6-inch FHD"},
	},
	{
		Slug: "dell-latitude-7440", Manufacturer: "Dell", Name: "Latitude 7440", ModelNumber: "7440-UL", Kind: "laptop",
		UnitCostMinor: 154900, CriticalityScore: 4,
		Specifications: map[string]string{"cpu": "Intel Core i7-1365U", "memory": "16 GB", "storage": "512 GB SSD", "display": "14-inch FHD+"},
	},
	{
		Slug: "lenovo-thinkpad-t14-gen3", Manufacturer: "Lenovo", Name: "ThinkPad T14 Gen 3", ModelNumber: "T14-G3", Kind: "laptop",
		UnitCostMinor: 119900, CriticalityScore: 4,
		Specifications: map[string]string{"cpu": "Intel Core i5-1145G7", "memory": "16 GB", "storage": "512 GB SSD", "display": "14-inch FHD"},
	},
	{
		Slug: "lenovo-thinkpad-t14", Manufacturer: "Lenovo", Name: "ThinkPad T14 Gen 5", ModelNumber: "T14-G5", Kind: "laptop",
		UnitCostMinor: 132900, CriticalityScore: 4,
		Specifications: map[string]string{"cpu": "Intel Core i5-1335U", "memory": "16 GB", "storage": "512 GB SSD", "display": "14-inch WUXGA"},
	},
	{
		Slug: "apple-ipad-air", Manufacturer: "Apple", Name: "iPad Air 11-inch M2", ModelNumber: "MUWE3LL/A", Kind: "tablet",
		UnitCostMinor: 79900, Specifications: map[string]string{"storage": "256 GB", "cellular": "Wi-Fi + Cellular", "display": "11-inch Liquid Retina"},
	},
	{
		Slug: "hp-elitebook-840", Manufacturer: "HP", Name: "EliteBook 840 G10", ModelNumber: "840-G10", Kind: "laptop",
		UnitCostMinor: 128900, Specifications: map[string]string{"cpu": "Intel Core i5-1335U", "memory": "16 GB", "storage": "512 GB SSD", "display": "14-inch WUXGA"},
	},
	{
		Slug: "dell-p2422h", Manufacturer: "Dell", Name: "P2422H Monitor", ModelNumber: "P2422H", Kind: "peripheral",
		UnitCostMinor: 21900, Specifications: map[string]string{"size": "24-inch", "resolution": "1920x1080", "ports": "HDMI, DisplayPort, VGA"},
	},
	{
		Slug: "logitech-mk270", Manufacturer: "Logitech", Name: "MK270 Mouse and Keyboard Combo", ModelNumber: "MK270", Kind: "peripheral",
		UnitCostMinor: 2999, Specifications: map[string]string{"connection": "2.4 GHz wireless", "includes": "keyboard and mouse"},
	},
	{
		Slug: "dell-wd19", Manufacturer: "Dell", Name: "WD19 Docking Station", ModelNumber: "WD19", Kind: "peripheral",
		UnitCostMinor: 18900, Specifications: map[string]string{"power": "130W", "video": "dual 4K", "connection": "USB-C"},
	},
}

var campusLabs = []labDef{
	{
		Slug: "studio-arts-mac", Name: "Studio Arts Mac Lab", BuildingSlug: "education", RoomNumber: "ED-410",
		MachineCount: 24, ModelSlug: "imac-m3", DepartmentSlug: "studio-arts",
		SoftwareNotes: "Adobe Photoshop 2026, Illustrator 2026, Creative Cloud site license; macOS Sonoma",
	},
	{
		Slug: "graphic-design-mac", Name: "Graphic Design Mac Lab", BuildingSlug: "education", RoomNumber: "ED-420",
		MachineCount: 30, ModelSlug: "mac-studio-m2", DepartmentSlug: "graphic-design",
		SoftwareNotes: "Adobe Creative Cloud full suite, Figma desktop, macOS Sonoma",
	},
	{
		Slug: "music-production", Name: "Music Production Lab", BuildingSlug: "education", RoomNumber: "ED-430",
		MachineCount: 12, ModelSlug: "mac-studio-m2", DepartmentSlug: "music",
		SoftwareNotes: "Logic Pro, Pro Tools, Ableton Live suite; macOS Sonoma",
	},
	{
		Slug: "general-windows-b", Name: "Education General Lab", BuildingSlug: "education", RoomNumber: "ED-105",
		MachineCount: 36, ModelSlug: "dell-optiplex-7020", DepartmentSlug: "school-education",
		SoftwareNotes: "Microsoft Office 365, classroom presentation tools; Windows 11 Pro",
	},
	{
		Slug: "cs-programming", Name: "Computer Science Programming Lab", BuildingSlug: "education", RoomNumber: "ED-510",
		MachineCount: 32, ModelSlug: "dell-precision-7865", DepartmentSlug: "computer-science",
		SoftwareNotes: "Visual Studio 2022, Docker Desktop, WSL2 Ubuntu, JetBrains suite; Windows 11 Pro",
	},
	{
		Slug: "cybersecurity", Name: "Cybersecurity Lab", BuildingSlug: "education", RoomNumber: "ED-520",
		MachineCount: 20, ModelSlug: "dell-precision-7865", DepartmentSlug: "cybersecurity",
		SoftwareNotes: "Isolated VLAN; Kali Linux VMs, Wireshark, Burp Suite Pro; Windows 11 Pro host",
	},
	{
		Slug: "digital-media-mac", Name: "Digital Media Mac Lab", BuildingSlug: "education", RoomNumber: "ED-530",
		MachineCount: 18, ModelSlug: "macbook-pro-m3", DepartmentSlug: "graphic-design",
		SoftwareNotes: "Final Cut Pro, Adobe Premiere Pro 2026, Logic Pro; macOS Sonoma",
	},
	{
		Slug: "autocad-engineering", Name: "AutoCAD & Design Engineering Lab", BuildingSlug: "sciences", RoomNumber: "SCI-140",
		MachineCount: 28, ModelSlug: "dell-precision-7865", DepartmentSlug: "engineering",
		SoftwareNotes: "Autodesk AutoCAD 2026, Revit 2026, SolidWorks 2026; Windows 11 Pro",
	},
	{
		Slug: "architecture-lab", Name: "Architecture Design Lab", BuildingSlug: "sciences", RoomNumber: "SCI-150",
		MachineCount: 22, ModelSlug: "dell-precision-7865", DepartmentSlug: "architecture",
		SoftwareNotes: "Autodesk AutoCAD 2026, Rhino 8, dual 27-inch displays; Windows 11 Pro",
	},
	{
		Slug: "nursing-simulation", Name: "Nursing Simulation Lab", BuildingSlug: "sciences", RoomNumber: "SCI-320",
		MachineCount: 16, ModelSlug: "dell-latitude-5540", DepartmentSlug: "nursing",
		SoftwareNotes: "Patient simulation software, Epic training module; Windows 11 Pro",
	},
	{
		Slug: "stem-research", Name: "STEM Research Lab", BuildingSlug: "sciences", RoomNumber: "SCI-410",
		MachineCount: 15, ModelSlug: "dell-precision-7865", DepartmentSlug: "physics",
		SoftwareNotes: "MATLAB, Mathematica, Python data science stack; Windows 11 Pro",
	},
	{
		Slug: "language-lab", Name: "Language Lab", BuildingSlug: "math-history", RoomNumber: "MH-115",
		MachineCount: 25, ModelSlug: "dell-optiplex-7020", DepartmentSlug: "english",
		SoftwareNotes: "Rosetta Stone campus license, headset integration; Windows 11 Pro",
	},
	{
		Slug: "general-windows-a", Name: "Library General Lab", BuildingSlug: "library", RoomNumber: "LIB-110",
		MachineCount: 40, ModelSlug: "dell-optiplex-7020", DepartmentSlug: "library-services",
		SoftwareNotes: "Microsoft Office 365, web browsers; Windows 11 Pro",
	},
	{
		Slug: "open-access", Name: "Open Access Lab", BuildingSlug: "library", RoomNumber: "LIB-120",
		MachineCount: 50, ModelSlug: "dell-optiplex-7020", DepartmentSlug: "library-services",
		SoftwareNotes: "Mixed general-purpose fleet; Microsoft Office 365; Windows 11 Pro",
	},
	{
		Slug: "mobile-checkout", Name: "Mobile Device Checkout Pool", BuildingSlug: "services", RoomNumber: "SVC-025",
		MachineCount: 60, ModelSlug: "dell-latitude-7440", DepartmentSlug: "it-services",
		SoftwareNotes: "Standard faculty/staff image; loaner pool managed by IT Services",
	},
}

type classroomDef struct {
	Slug           string
	Name           string
	BuildingSlug   string
	RoomNumber     string
	DepartmentSlug string
}

type residenceRoomDef struct {
	Slug       string
	Name       string
	RoomNumber string
	BedCount   int
}

const (
	ResidenceBuildingSlug = "residence-hall"
	DormResidentCount     = 800
)

var campusClassrooms = []classroomDef{
	{Slug: "edu-lecture-a", Name: "Education Lecture Hall A", BuildingSlug: "education", RoomNumber: "ED-130", DepartmentSlug: "school-education"},
	{Slug: "edu-lecture-b", Name: "Education Lecture Hall B", BuildingSlug: "education", RoomNumber: "ED-140", DepartmentSlug: "school-business"},
	{Slug: "edu-seminar", Name: "Computing Seminar Room", BuildingSlug: "education", RoomNumber: "ED-150", DepartmentSlug: "computer-science"},
	{Slug: "edu-studio-critique", Name: "Studio Critique Classroom", BuildingSlug: "education", RoomNumber: "ED-160", DepartmentSlug: "studio-arts"},
	{Slug: "edu-design-critique", Name: "Design Critique Classroom", BuildingSlug: "education", RoomNumber: "ED-170", DepartmentSlug: "graphic-design"},
	{Slug: "edu-music-rehearsal", Name: "Music Rehearsal Classroom", BuildingSlug: "education", RoomNumber: "ED-180", DepartmentSlug: "music"},
	{Slug: "sci-lecture-a", Name: "Sciences Lecture Hall A", BuildingSlug: "sciences", RoomNumber: "SCI-101", DepartmentSlug: "biology"},
	{Slug: "sci-lecture-b", Name: "Sciences Lecture Hall B", BuildingSlug: "sciences", RoomNumber: "SCI-102", DepartmentSlug: "chemistry"},
	{Slug: "sci-engineering", Name: "Engineering Classroom", BuildingSlug: "sciences", RoomNumber: "SCI-103", DepartmentSlug: "engineering"},
	{Slug: "sci-nursing", Name: "Nursing Classroom", BuildingSlug: "sciences", RoomNumber: "SCI-104", DepartmentSlug: "nursing"},
	{Slug: "sci-architecture", Name: "Architecture Studio Classroom", BuildingSlug: "sciences", RoomNumber: "SCI-105", DepartmentSlug: "architecture"},
	{Slug: "sci-physics", Name: "Physics Classroom", BuildingSlug: "sciences", RoomNumber: "SCI-106", DepartmentSlug: "physics"},
	{Slug: "mh-math-a", Name: "Mathematics Lecture A", BuildingSlug: "math-history", RoomNumber: "MH-130", DepartmentSlug: "mathematics"},
	{Slug: "mh-math-b", Name: "Mathematics Lecture B", BuildingSlug: "math-history", RoomNumber: "MH-140", DepartmentSlug: "mathematics"},
	{Slug: "mh-history", Name: "History Seminar Room", BuildingSlug: "math-history", RoomNumber: "MH-150", DepartmentSlug: "history"},
	{Slug: "mh-english", Name: "English Classroom", BuildingSlug: "math-history", RoomNumber: "MH-160", DepartmentSlug: "english"},
	{Slug: "mh-sociology", Name: "Social Sciences Classroom", BuildingSlug: "math-history", RoomNumber: "MH-170", DepartmentSlug: "sociology"},
	{Slug: "mh-philosophy", Name: "Philosophy Seminar Room", BuildingSlug: "math-history", RoomNumber: "MH-180", DepartmentSlug: "philosophy"},
}

func campusResidenceRooms() []residenceRoomDef {
	rooms := make([]residenceRoomDef, 0, 40)
	for index := 1; index <= 40; index++ {
		number := fmt.Sprintf("RH-%03d", index)
		rooms = append(rooms, residenceRoomDef{
			Slug: fmt.Sprintf("residence-%03d", index), Name: "Residence Hall " + number,
			RoomNumber: number, BedCount: 20,
		})
	}
	return rooms
}

var employeeFirstNames = []string{
	"James", "Maria", "Robert", "Patricia", "Michael", "Jennifer", "William", "Linda", "David", "Elizabeth",
	"Richard", "Barbara", "Joseph", "Susan", "Thomas", "Jessica", "Christopher", "Sarah", "Daniel", "Karen",
	"Matthew", "Lisa", "Anthony", "Nancy", "Mark", "Betty", "Donald", "Margaret", "Steven", "Sandra",
	"Andrew", "Ashley", "Paul", "Kimberly", "Joshua", "Emily", "Kenneth", "Donna", "Kevin", "Michelle",
	"Brian", "Carol", "George", "Amanda", "Timothy", "Melissa", "Ronald", "Deborah", "Edward", "Stephanie",
	"Jason", "Rebecca", "Jeffrey", "Laura", "Ryan", "Sharon", "Jacob", "Cynthia", "Gary", "Kathleen",
	"Nicholas", "Amy", "Eric", "Angela", "Jonathan", "Shirley", "Stephen", "Anna", "Larry", "Brenda",
	"Justin", "Pamela", "Scott", "Emma", "Brandon", "Nicole", "Benjamin", "Helen", "Samuel", "Samantha",
	"Gregory", "Katherine", "Alexander", "Christine", "Patrick", "Debra", "Frank", "Rachel", "Raymond", "Carolyn",
	"Jack", "Janet", "Dennis", "Catherine", "Jerry", "Maria", "Tyler", "Heather", "Aaron", "Diane",
}

var employeeLastNames = []string{
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
}

type seededEmployee struct {
	ID             string
	DepartmentSlug string
	BuildingSlug   string
	OfficeSlug     string
}

type seededStudent struct {
	ID             string
	Active         bool
	VisualArts     bool
	DisplayTag     string
	DepartmentSlug string
}

type seededPerson struct {
	ID             string
	DepartmentSlug string
	Kind           string // employee | active-student | inactive-student
	VisualArts     bool
}

func departmentBySlug(slug string) departmentDef {
	for _, department := range campusDepartments {
		if department.Slug == slug {
			return department
		}
	}
	return campusDepartments[0]
}

func officeByDepartment(slug string) officeDef {
	for _, office := range campusOffices {
		if office.DepartmentSlug == slug {
			return office
		}
	}
	return campusOffices[0]
}

func isTeachingDepartment(department departmentDef) bool {
	switch department.School {
	case "Academic", "Business & Technology", "Arts & Humanities", "Science & Engineering", "Health Sciences", "Arts & Sciences":
		return true
	default:
		return false
	}
}

func employeeDepartments() []departmentDef {
	total := 0
	for _, department := range campusDepartments {
		total += department.Headcount
	}
	if total == 0 {
		return campusDepartments
	}
	expanded := make([]departmentDef, 0, EmployeeCount)
	for _, department := range campusDepartments {
		slots := department.Headcount * EmployeeCount / total
		if slots < 1 && department.Headcount > 0 {
			slots = 1
		}
		for count := 0; count < slots; count++ {
			expanded = append(expanded, department)
		}
	}
	for len(expanded) < EmployeeCount {
		expanded = append(expanded, campusDepartments[len(expanded)%len(campusDepartments)])
	}
	return expanded[:EmployeeCount]
}
