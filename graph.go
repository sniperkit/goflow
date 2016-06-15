package flow

type Graph struct {
    // Block Data
    name        string
    nodes       InstanceMap
    edges       EdgeMap
    rev_edges   map[ParamAddress]ParamAddress  // Used to lookup reverse edges

    // Block Inputs
    infeed      ParamLstMap    // Connects name of a param in inputs to a ParamAddress of some parameter in some node
    outfeed     ParamMap    // Connects name of a param in outputs to a ParamAddress of some parameter in some node
    inputs      ParamTypes
    outputs     ParamTypes
    
    // Other features
    constants   map[ParamAddress]interface{}
}

func NewGraph(name string, inputs, outputs ParamTypes) (*Graph, *Error) {
    // Handle Errors
    nilGraph := &Graph{}
    switch {
        case len(inputs) <= 0:
            return nilGraph, nil
        case len(outputs) <= 0:
            return nilGraph, nil
    }
    
    nodes, edges    := make(InstanceMap), make(EdgeMap)
    infeed, outfeed := make(ParamLstMap), make(ParamMap)
    rev_edges       := make(map[ParamAddress]ParamAddress)
    constants       := make(map[ParamAddress]interface{})
    return &Graph{name, nodes, edges, rev_edges, infeed, outfeed, inputs, outputs, constants}, nil
}

func (g Graph) AddFeed(name string, t Type, is_input bool) (err *Error) {
    wrapper := func(X ParamTypes) *Error {
        t2, exists := X[name]
        if exists {
            if !CheckSame(t, t2) {
                return &Error{TYPE_ERROR, "This parameter already exists in a different type."}
            } else {
                return &Error{ALREADY_EXISTS_ERROR, "This parameter already exists."}
            }
        } else {
            X[name] = t
        }
        return nil
    }

    if is_input {
        err = wrapper(g.inputs)
    } else {
        err = wrapper(g.outputs)
    }
    return
}

func (g Graph) AddConstant(val interface{}, param_addr Address, param_name string) *Error {
    param, p_exists := g.FindParam(param_name, param_addr)
    if p_exists {
        type_ok := CheckType(param.T, val)
        linked := g.GetEdges(param) // FIXME: Check if parameter is already connected to
        switch {
            case !param.is_input:
                return &Error{NOT_INPUT_ERROR, "Parameter must be an input."}
            case !type_ok:
                return &Error{TYPE_ERROR, "Parameter is not the same type as val."}
            case len(linked)!=0:
                return &Error{ALREADY_EXISTS_ERROR, "Parameter is already linked."}
            default:
                g.constants[param] = val
                return nil
        }
    } else {
        return &Error{DNE_ERROR, "Parameter does not exist."}
    }
}

func (g Graph) AddNode(blk FunctionBlock, addr Address) (ok *Error) {
    _, exists := g.nodes[addr]
    if !exists {
        g.nodes[addr] = blk
        ok = nil
    } else {
        ok = &Error{DNE_ERROR, "blk is already a node in Graph."}
    }
    return
}

// out_addr[out_param_name] -> in_addr[in_param_name]
func (g Graph) AddEdge(out_addr Address, out_param_name string,
                       in_addr Address, in_param_name string) (ok bool) {
    //logger.Println("Adding Edge: ", out_addr, out_param_name, " -> ", in_addr, in_param_name)
    ok = false
    out_param, out_exists := g.FindParam(out_param_name, out_addr)  // Get the output parameters of out_blk
    in_param, in_exists := g.FindParam(in_param_name, in_addr)   // Get the input parameters of in_blk
    linked := g.GetEdges(in_param)                                // Check if there is already an edge connecting input
    if in_exists && out_exists && len(linked)==0 {      // If both exist
        if CheckSame(in_param.T, out_param.T) && in_param.is_input && !out_param.is_input {
            g.edges[out_param] = append(g.edges[out_param], in_param)          // Append the new link to the edges under the out_param
            g.rev_edges[in_param] = out_param  // Add the reverse for reverse lookup
            ok = true
        }
    }
    return
}

func (g Graph) GetEdges(param ParamAddress) []ParamAddress {
    edges_out, exists := g.edges[param]
    if !exists {
        rev_out, rev_exists := g.rev_edges[param]
        if !rev_exists {
            return []ParamAddress{}
        }
        return []ParamAddress{rev_out}
    } else {
        return edges_out
    }
}

func (g Graph) FindParam(name string, addr Address) (param ParamAddress, exists bool) {
    in_params, out_params := g.nodes[addr].GetParams()
    in_t, in_exists := in_params[name]
    out_t, out_exists := out_params[name]
    switch {
        case in_exists == out_exists:
            exists = false
        case in_exists:
            param = ParamAddress{name, addr, in_t, true}
            exists = true
        case out_exists:
            param = ParamAddress{name, addr, out_t, false}
            exists = true
    }
    return
}

// self[self_param_name] -> in_addr[in_param_name]
// FIXME: Needs to check if this is already linked
func (g Graph) LinkIn(self_param_name string, in_param_name string, in_addr Address) (ok bool) {
    param, exists := g.FindParam(in_param_name, in_addr)
    if exists && CheckSame(g.inputs[self_param_name], param.T) {
        g.infeed[self_param_name] = append(g.infeed[self_param_name], ParamAddress{in_param_name, in_addr, param.T, true})
        return true
    } else {
        return false
    }
}

// out_addr[out_param_name] -> self[self_param_name]
// FIXME: Needs to check if outfeed already linked
func (g Graph) LinkOut(out_addr Address, out_param_name string, self_param_name string) (ok bool) {
    param, exists := g.FindParam(out_param_name, out_addr)
    if exists && CheckSame(g.outputs[self_param_name], param.T) {
        g.outfeed[self_param_name] = ParamAddress{out_param_name, out_addr, param.T, false}
        return true
    } else {
        return false
    }
}

// Returns copies of all parameters in FunctionBlock
func (g Graph) GetParams() (inputs ParamTypes, outputs ParamTypes) {
    return CopyTypes(g.inputs), CopyTypes(g.outputs)
}

// Returns a copy of FunctionBlock's InstanceId
func (g Graph) GetName() string {return g.name}

func (g Graph) Run(inputs ParamValues,
                   outputs chan DataOut,
                   stop chan bool,
                   err chan *FlowError, id InstanceID) {
    logger := CreateLogger("none", "[INFO]")
    
    ADDR := Address{g.GetName(), id}
    logger.Println("Running Graph: ", ADDR)
    
    // Check types to ensure inputs are the type defined in input parameters
    chk_exists := checkInputs(inputs, g.inputs)
    chk_types  := CheckTypes(inputs, g.inputs)
    switch {
        case !chk_exists:
            err <- NewFlowError(DNE_ERROR, "Not all inputs satisfied.", ADDR)
            return
        case !chk_types:
            err <- NewFlowError(TYPE_ERROR, "Inputs are impropper types.", ADDR)
            return
    }

    // Declare variables
    running       := true                           // When this turns to false, the process stops
    all_waiting   := make(InstanceMap)              // A map of all blocks waiting for inputs
    all_running   := make(InstanceMap)              // A map of all blocks we are waiting to return data
    all_suspended := make(InstanceMap)              // A map of all blocks with blocked outputs waiting to be shifted down the graph
    all_data_in   := make(map[ParamAddress]interface{})  // A map of all data waiting at the inputs of each block
    all_data_out  := make(map[ParamAddress]interface{})  // A map of all data waiting at the outputs of each block
    all_stops     := make(map[Address](chan bool))  // A map of all stop channels passed to each running block
    flow_errs     := make(chan *FlowError)           // A channel passed to each running block to send back errors
    data_flow     := make(chan DataOut)             // A channel passed to each running block to send back return data
    graph_out     := make(ParamValues)              // The output for the entire graph

    // Put all nodes in waiting
    logger.Println("Putting all nodes in waiting.")
    for addr, blk := range g.nodes {
        all_waiting[addr] = blk
    }

    // Create some functions for simplified code structure
    logger.Println("Defining Functions")
    // Stops all children blocks
    stopAll := func() {
        logger.Println("Stopping all.")
        // Push stop down to all subfunctions
        for _, val := range all_stops {
            val <- true
        }
        running = false  // Stop this block as well
    }
    
    // Pushes an error up the pipeline and stops all blocks
    pushError := func(class int, info string) {
        logger.Println("Pushing Error: ", info)
        flow_errs <- NewFlowError(class, info, ADDR)
        stopAll()
    }
    
    // Adds data to all_data_in, creates ParamValues struct if necessary.
    handleInput := func(param ParamAddress, val interface{}) (ok bool) {
        logger.Println("Handling Inputs.")
        logger.Println("Param: ", param, "Val: ", val)
        _ , param_exists := all_data_in[param]                        // Get the input value and check if it exists
        if !param_exists {                                            // Check if it exists
            ok = true                                                  // If it is ok, then return true
            all_data_in[param] = val                                   // Add addr,val to the preexisting map
        }
        //logger.Println("Check: ", all_data_in[param], all_data_in[param] == val)
        return
    }
    
    // Adds data to graph_out, pushes error if type is wrong or if out already set
    // Adds data to all_data_out
    // Deletes from all_running and adds to all_waiting
    handleOutput := func(vals DataOut) {
        logger.Println("Handling Output: ", vals)
        V    := vals.Values                     // Get values for easy access
        addr := vals.Addr                       // Get address for easy access
        blk  := all_running[addr]               // Retrieve the block from running
        _, out_params := blk.GetParams()        // Get blk output param types
        for param_name, t := range out_params {
            param := ParamAddress{param_name, addr, t, false}
            val, val_exists := V[param_name] // Set the output data
            if val_exists {                  // Only set val if it exists
                all_data_out[param] = val
            } else {
                pushError(NOT_READY_ERROR, "All Data Output not present.")
                return
            }
        }
        delete(all_running, addr)           // Delete block from running
        all_suspended[addr] = blk           // Add block to suspended
        delete(all_stops, addr)             // Delete channels
    }
    
    // Iterates through all given inputs and adds them to method's all_data_ins.
    loadvars := func() {
        // Main loop
        logger.Println("Loading Variables.")
        for name , param_lst := range g.infeed {         // Iterate through the names/values given in function parameters
            for _, node_param := range param_lst {
                val, exists := inputs[name]          // Lookup this parameter in the graph inputs
                if exists {                          // If the parameter does exist
                    handleInput(node_param, val)     // Add the value to all_data_in
                    logger.Println(node_param, val, all_data_in[node_param])
                } else {                             // Otherwise, error
                    pushError(DNE_ERROR, "Input parameter does not exist.")
                    return
                }
            }
        }
    }

    // Loads constants into input data
    loadconstants := func() {
        logger.Println("Loading Constants")
        for param, val := range g.constants {
            handleInput(param, val)
        }
    }

    // Iterate through all blocks that are waiting
    // to see if all of their inputs have been set.
    // If so, it runs them...
    // Deleting them from waiting, and placing them in running.
    checkWaiting := func() (ran bool) {
        logger.Println("Checking Waiting.")
        
        //logger.Println("Data In: ", all_data_in)
        // Runs a block and moves it from waiting to running, catalogues all channels
        blockRun := func(addr Address, blk FunctionBlock, f_in ParamValues) {
            logger.Println("Running ", blk.GetName())
            f_stop := make(chan bool)                                // Make a stop channel
            go blk.Run(f_in, data_flow, f_stop, flow_errs, addr.ID)  // Run the block as a goroutine
            delete(all_waiting, addr)                                // Delete the block from waiting
            all_running[addr] = blk                                  // Add the block to running
            all_stops[addr] = f_stop                                // Add stop channel to map
            
        }
        
        // Main loop
        ran = false
        for addr, blk := range all_waiting {                // Iterate through all waiting
            in_params, _ := blk.GetParams()                 // Get the inputs from the block
            f_in := make(ParamValues)
            ready := true
            for param_name, t := range in_params {
                param := ParamAddress{param_name, addr, t, true}
                in_val, val_exists := all_data_in[param]        // Get their stored values
                if val_exists {
                    f_in[param_name] = in_val
                } else {
                    ready = false
                }
            }
            if ready {
                blockRun(addr, blk, f_in)  // If so, then run the block.
                ran = true                 // Indicate that we have indeed ran at least one block.
            }
        }
        return ran
    }
    
    // Monitor the data_flow channel for the next incoming data.
    // Blocks until some packet is received, either data, stop, or error 
    checkRunning := func() (found bool) {
        logger.Println("Checking running")
        // Wait for some data input
        found = false
        done := false
        for running && !done {                  // Do not begin waiting if this block is not running
            logger.Println("Waiting for data input.")
            select {                            // Wait for input
                case data := <- data_flow:      // If there is data input
                    handleOutput(data)          // Handle it
                    found = true                // Declare data was found
                    done = true
                case e := <- flow_errs:         // If it is an error
                    logger.Println("Error Returned: ", e)
                    pushError(e.Class, e.Info)  // If it is dangerous, push the error
                    done = true
                case <-stop:                    // If a stop is received
                    stopAll()                   // Stop all processes
                    done = true
            }
        }
        return
    }
    
    // Iterate through all blocks that are suspended
    // to see if all of their outputs have been set.
    // If so, it runs them...
    // Deleting them from waiting, and placing them in running.
    checkSuspended := func() {
        for addr, blk := range all_suspended {                         // Iterate through all suspended blocks
            _, out_p_map := blk.GetParams()                            // Get the parameters of the block
            ready := true
            for name, t := range out_p_map {
                param := ParamAddress{name, addr, t, false}
                _, exists := all_data_out[param]
                if exists {
                    ready = false
                }
            }
            if ready {
                delete(all_suspended, addr)
                all_waiting[addr] = blk
                f_in, _ := blk.GetParams()
                // Delete all inputs from all_data_in
                for param_name, _ := range f_in {
                    param, _ := g.FindParam(param_name, addr)
                    delete(all_data_in, param)
                }
            }
        }
    }
    
    // Shift outputs to inputs based on graph, and also to graph_out
    shiftData := func() (success bool) {
        
        // Shift data from all_data_out to all_data_in
        logger.Println("Shifting Data")
        for out_p, val := range all_data_out {
            logger.Println("HERE", out_p, g.edges[out_p])
            for _, in_p := range g.edges[out_p] {              // Iterate through all linked inputs
                ok := handleInput(in_p, val)                   // Check the type and add it to all_data_in
                if ok {
                    delete(all_data_out, out_p)                // Delete the parameter from all_data_out
                } else {
                    return false                               // Handle errors
                }    
            }
        }
        
        // Shift data to the graph_out by iterating through the outfeed
        logger.Println("Shifting Data to Output")
        for self_param_name, out_param := range g.outfeed {
            val, exists := all_data_out[out_param]
            if exists {
                graph_out[self_param_name] = val
                logger.Println("Graph Out: ", val, graph_out[self_param_name])
            }
        }
        return true
    }
    
    // Returns true if all parameters in g.outputs referenced in graph_out
    checkDone := func() (done bool) {
        logger.Println("Checking Done")
        for name, _ := range g.outfeed {                // Iterate through all output parameters
            _, exists := graph_out[name]                // Check if each parameters exist in graph_out
            if !exists {return false}                   // If any does not exist, immediately return false
        }
        logger.Println("DONE!!!")
        return true                                     // If you pass the loop, all exist, return true
    }
    
    logger.Println("Done Defining Functions")
    
    // Main Logic
    loadvars()                   // Begin by loading all inputs into the data maps and adding all blocks to waiting
    logger.Println("Done Loading")
    for running {
        logger.Println("Running..")
        loadconstants()              // Load all constants before running any block.
        logger.Println("Constants Loaded")
        checkWaiting()               // Then run waiting blocks that have all inputs availible
        logger.Println("Data: ", len(all_data_in))
        logger.Println("Running: ", len(all_running))
        checkRunning()               // Then, wait for some return on a running block
        shiftData()                  // Try to shift outputs to linked inputs
        if checkDone() {             // See if the output map contains enough data to be done
            stopAll()                // Stop all processes
            outputs <- DataOut{Addr: ADDR, Values: graph_out} // And return our outputs
        } else {                     // If we are not done
            checkSuspended()         // Then, move from the outputs to the inputs and graph_out, and make methods waiting again
        }    
    }
}

// Checks if all keys in params are present in values
// And that all values are of their appropriate types as labeled in in params
func (g Graph) checkTypes(values ParamValues, params ParamMap) (ok bool) {
    for name, param := range params {
        val, exists := values[name]
        if !exists || !CheckType(param.T, val) {
            return false
        }
    }
    return true
}